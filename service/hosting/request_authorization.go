package hosting

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

// 请求断言只用于同机挂靠控制面；不能当作控制台登录或模型 API Key。
// 执行层只向 Oak 查询验证结果，不持有派生密钥，因此无法自行签发授权。
const requestAuthorizationUse = "hosting_control_request"
const requestAuthorizationIssuer = "oak-new-api"
const requestAuthorizationAudience = "oak-gpt-load-hosting"
const requestAuthorizationLifetime = 30 * time.Second // 同机核验不需要延长成长期会话。

// RequestBinding 必须由执行层从实际 HTTP 请求计算，不能直接相信客户端声明。
// URI 包括原始 owner_ref 查询串；正文按原始字节计算小写 SHA-256。
// 幂等键属于授权的一部分，不能拿同一断言换一个新的操作 UUID。
type RequestBinding struct {
	Method         string `json:"method"`          // 大写方法精确匹配，禁止把读权限改成写权限。
	RequestURI     string `json:"request_uri"`     // 不包含主机，目的服务由固定 audience 约束。
	BodySHA256     string `json:"body_sha256"`     // 空正文也必须使用空字节串的摘要。
	IdempotencyKey string `json:"idempotency_key"` // 空值也参与匹配，不允许验证后补写。
}

// requestAuthorizationClaims 与用户登录 JWT 使用不同用途、受众和派生密钥。
// subject 是 Oak 用户编号，owner_ref 必须与该编号的服务端映射完全相同。
// 本断言不是单次证明：有效期内可以重复验证，业务写入依靠既有 UUID 幂等。
type requestAuthorizationClaims struct {
	jwt.RegisteredClaims        // iat、nbf、exp 均必需，不能接受无期限的签名声明。
	RequestBinding              // 不可省略的完整请求边界，验证端逐字段精确匹配。
	OwnerRef             string `json:"owner_ref"` // 返回给执行层的唯一业务授权信息。
	Use                  string `json:"use"`       // 用途不能从普通登录令牌中推导。
}

// requestAuthorizationKey 从显式持久密钥派生专用 MAC 密钥，不共享登录签名用途。
// 不采用进程启动时随机生成的 CryptoSecret，避免重启后出现不可解释的变化。
// 长期密钥始终留在 New API，gpt-load 与浏览器均不会获得它。
// 使用 HMAC 的固定用途上下文，隔离同一主密钥下的账号密文与请求签名。
func requestAuthorizationKey() ([]byte, error) {
	secret := os.Getenv("CRYPTO_SECRET")
	if len(secret) < 32 {
		return nil, &Error{503, "hosting_encryption_not_configured"} // 配置失败不能降级成匿名控制。
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("oak-hosting-request-authorization-v1")) // hash.Hash.Write 按契约不会失败。
	return mac.Sum(nil), nil
}

// validRequestBinding 约束内部签发器只能为挂靠命名空间和确切所有者授权。
// 相对路径、点段、重复 query 或额外 query 都不能改变固定控制面目的。
// 不接受任意 URL，也不允许调用方把 owner_ref 绑定成他人或公共管理身份。
// 正文摘要和幂等键设长度上限，避免验证入口接收无界控制字段。
func validRequestBinding(binding RequestBinding, owner string) bool {
	if binding.Method != http.MethodGet && binding.Method != http.MethodPost && binding.Method != http.MethodPatch && binding.Method != http.MethodDelete {
		return false // 不为尚未支持的方法预留隐含授权。
	}
	if len(binding.RequestURI) > 2048 || len(binding.IdempotencyKey) > 128 || len(binding.BodySHA256) != sha256.Size*2 {
		return false
	}
	parsed, err := url.ParseRequestURI(binding.RequestURI)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Fragment != "" {
		return false
	}
	// 解码后的点段也要拒绝，不能让代理规范化后跳出 hosting/v1。
	if !strings.HasPrefix(parsed.Path, "/api/hosting/v1/") || path.Clean(parsed.Path) != parsed.Path {
		return false
	}
	if parsed.RawQuery != "owner_ref="+url.QueryEscape(owner) {
		return false // 单值、编码和顺序固定，避免不同组件解析出不同归属。
	}
	decoded, err := hex.DecodeString(binding.BodySHA256)
	return err == nil && hex.EncodeToString(decoded) == binding.BodySHA256 // 大写或非十六进制摘要不是本协议。
}

// signRequestAuthorization 仅由控制面 Client.Call 调用，不暴露公开签发接口。
// 对实际将发送的序列化正文签名，不以解析后重新编码的正文替代。
// 带 owner_ref 的创建正文必须与服务端归属一致，不能只绑定查询参数。
// 签发之后只允许添加 Authorization，不再修改方法、URI、正文和幂等键。
func signRequestAuthorization(request *http.Request, userID int, body []byte) (string, error) {
	owner := OwnerRef(userID) // 归属仅由已授权业务上下文给出的用户编号生成。
	digest := sha256.Sum256(body)
	binding := RequestBinding{Method: request.Method, RequestURI: request.URL.RequestURI(), BodySHA256: hex.EncodeToString(digest[:]), IdempotencyKey: request.Header.Get("Idempotency-Key")}
	if userID <= 0 || !validRequestBinding(binding, owner) {
		return "", &Error{500, "hosting_invalid_internal_request"} // 内部契约错误不应对外发送请求。
	}
	if len(body) > 0 {
		var payload struct {
			OwnerRef *string `json:"owner_ref"` // 非创建动作可不携带，但携带时必须一致。
		}
		if common.Unmarshal(body, &payload) != nil || (payload.OwnerRef != nil && *payload.OwnerRef != owner) {
			return "", &Error{500, "hosting_invalid_internal_request"}
		}
	}
	key, err := requestAuthorizationKey()
	if err != nil {
		return "", err
	}
	now := time.Now() // 同次获取时间，避免跨秒构造出不同的 iat 与 nbf。
	claims := requestAuthorizationClaims{
		RegisteredClaims: jwt.RegisteredClaims{Issuer: requestAuthorizationIssuer, Audience: jwt.ClaimStrings{requestAuthorizationAudience}, Subject: strconv.Itoa(userID), IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(requestAuthorizationLifetime))},
		RequestBinding:   binding, OwnerRef: owner, Use: requestAuthorizationUse,
	}
	// 专用 typ 进一步隔离不同 JWT 消费入口；算法不能由请求参数选择。
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["typ"] = "oak-hosting-request+jwt"
	return token.SignedString(key)
}

// VerifyRequestAuthorization 只验证精确请求断言与用户当前状态，不签发新票据。
// 验证过程不修改业务数据、不消费 nonce，也不访问 gpt-load 或原厂服务。
// 有效期内同一断言可重复核验；这不替代接入证明或重置操作的单次保护。
// 错算法、用途、期限、内容或所有者都返回统一 401，避免暴露匹配细节。
// 固定 keyfunc 不使用令牌中的远端 key URL，阻断算法与密钥来源混淆。
// 数据库故障显式返回 503；用户禁用、删除或不存在立即停止继续授权。
func VerifyRequestAuthorization(raw string, binding RequestBinding) (string, error) {
	denied := &Error{http.StatusUnauthorized, "hosting_request_unauthorized"}
	if raw == "" || len(raw) > 4096 {
		return "", denied // 先限制长度，再进行昂贵的解码或数据库查询。
	}
	key, err := requestAuthorizationKey()
	if err != nil {
		return "", err
	}
	claims := &requestAuthorizationClaims{}
	parsed, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		return key, nil // 不读取 kid、jku 或 jwk 指定的任何外部密钥。
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer(requestAuthorizationIssuer), jwt.WithAudience(requestAuthorizationAudience), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil || !parsed.Valid || parsed.Header["typ"] != "oak-hosting-request+jwt" || claims.Use != requestAuthorizationUse {
		return "", denied // 不把解析器详细错误暴露给持票者。
	}
	// 期限是本协议固定约束，签名正确也不能把 30 秒票据延长为长期管理凭据。
	if claims.IssuedAt == nil || claims.NotBefore == nil || claims.ExpiresAt == nil || len(claims.Audience) != 1 {
		return "", denied
	}
	lifetime := claims.ExpiresAt.Sub(claims.IssuedAt.Time) // JWT 已验证当前时刻，这里限制票据总寿命。
	if lifetime <= 0 || lifetime > requestAuthorizationLifetime || !claims.NotBefore.Equal(claims.IssuedAt.Time) {
		return "", denied // 过长期或不一致的时间边界不能扩大授权窗口。
	}
	userID, err := strconv.Atoi(claims.Subject)
	if err != nil || userID <= 0 || claims.OwnerRef != OwnerRef(userID) || claims.RequestBinding != binding || !validRequestBinding(binding, claims.OwnerRef) {
		return "", denied
	}
	// 直接读当前用户状态，不用可能滞后的缓存；额度耗尽不等于身份被禁用。
	var user model.User
	err = model.DB.Select("id", "status").First(&user, userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && user.Status != common.UserStatusEnabled) {
		return "", denied
	}
	if err != nil {
		return "", &Error{http.StatusServiceUnavailable, "hosting_authorization_unavailable"}
	}
	return claims.OwnerRef, nil // 返回固定归属引用，不返回原始令牌或签名材料。
}
