// Package hosting 封装 Oak 对执行层的窄范围控制接口。
// 用户请求不能选择目标地址、owner_ref 或远端资源 ID。
package hosting

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// Error 只暴露稳定状态，不把管理接口的原始响应或网络地址带给用户。
type Error struct {
	Status int    // 仅用于公共 HTTP 结果，不携带原始上游状态正文。
	Code   string // 固定机器错误码，前端应按词条解释。
}

func (e *Error) Error() string { return e.Code }

// Client 使用部署配置访问同机执行层；禁止重定向泄漏管理密钥。
// 控制请求有独立超时，模型流式传输由调用方使用单独生命周期。
type Client struct {
	BaseURL string       // 部署地址已经过回环校验，禁止用户重写。
	key     string       // 管理凭据只保留在进程内存，不序列化。
	http    *http.Client // 共享传输用于控制请求，禁止跟随重定向。
}

// 控制请求共享连接池，避免每次列表查询创建无法回收的空闲连接。
// 禁止环境代理与重定向，管理密钥只能到达已校验的回环服务。
var controlHTTPClient = &http.Client{
	Transport: func() *http.Transport {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		return transport
	}(),
	Timeout:       45 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// NewClient 不在缺少配置时猜测管理凭据，未配置功能明确不可用。
// Loopback 限制符合当前双服务同机部署，不接受用户提供的 URL。
// 仅接受字面回环 IP，避免 DNS 重绑定和容器外出站绕过同机边界。
// 拒绝 URL 用户信息、查询和路径，防止管理凭据被拼到意外目标。
// 管理密钥缺失是功能未配置，不得退回公共接口或默认测试密钥。
// 此配置适用于当前同机部署；跨主机必须先设计受控传输和身份边界。
// Client 的共享连接池禁用环境代理，管理 Authorization 不会发给代理服务器。
// 拒绝 HTTP 重定向，因为重定向不是控制 API 协议的一部分。
// 模型流式调用不复用这里的控制超时，以免长响应被管理设置截断。
// 地址是部署配置，所有浏览器输入都不能覆盖它。
func NewClient() (*Client, error) {
	address := strings.TrimRight(os.Getenv("OAK_GPT_LOAD_URL"), "/")
	if address == "" {
		address = "http://127.0.0.1:3001"
	}
	u, err := url.Parse(address)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, &Error{503, "hosting_not_configured"}
	}
	// 仅允许字面回环地址，避免 DNS 指向变化和透明出站代理。
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return nil, &Error{503, "hosting_not_configured"}
	}
	key := strings.TrimSpace(os.Getenv("OAK_GPT_LOAD_MANAGEMENT_KEY"))
	if key == "" {
		return nil, &Error{503, "hosting_not_configured"}
	}
	return &Client{BaseURL: address, key: key, http: controlHTTPClient}, nil
}

// OwnerRef 是服务端生成的非秘密归属引用，不接受浏览器覆盖。
// 稳定的命名空间区分 Oak 用户身份与执行层内部资源编号。
// 该引用不是密码，但仍只能由服务端根据已认证用户生成。
// 原厂邮箱和供应商账号 ID 不参与此字段，避免暴露私人身份。
// 控制面必须把该值与目标资源共同校验，不能只凭资源 UUID 放行。
func OwnerRef(userID int) string { return "oak:" + strconv.Itoa(userID) }

// Call 仅用于此包固定拼出的 hosting/v1 路径，返回值需由具体 DTO 白名单解析。
// 不自动重试写操作；超时保留调用方已有操作 UUID 供恢复。
// 管理路径只能由业务代码拼接，任何查询字符都按内部调用错误拒绝。
// owner_ref 始终从可信用户编号生成，不能由 payload 覆盖查询归属。
// JSON 序列化失败时不会发送请求，避免执行层接收半个业务意图。
// HTTP 超时表示结果可能已经发生，不能自动重试不可逆动作。
// 响应读取有大小上限，错误页也不能绕过这个内存边界。
// 只保留少数有业务意义的状态码，原始远端正文不会进入错误。
// 成功必须同时满足 HTTP 成功、code=0 和有效 data。
// 固定类型解码是第二层脱敏，未知字段不会被原样传回浏览器。
// 调用方必须按动作验证 data 的业务标识，传输成功不等于资源归属正确。
// 不检查 result 为空时的业务结构，只用于调用方明确不需要返回数据的动作。
func (client *Client) Call(ctx context.Context, method, path string, userID int, payload any, operationID string, result any) error {
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#") {
		return &Error{500, "hosting_invalid_internal_path"}
	}
	var body []byte
	var err error
	if payload != nil {
		body, err = common.Marshal(payload)
		if err != nil {
			return err
		}
	}
	address := client.BaseURL + "/api/hosting/v1" + path + "?owner_ref=" + url.QueryEscape(OwnerRef(userID))
	request, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(body))
	if err != nil {
		return &Error{502, "hosting_control_unavailable"}
	}
	request.Header.Set("Authorization", "Bearer "+client.key)
	request.Header.Set("Content-Type", "application/json")
	if operationID != "" {
		request.Header.Set("Idempotency-Key", operationID)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return &Error{502, "hosting_control_outcome_unknown"}
	}
	defer response.Body.Close()
	// 有界读取也保护错误响应，不能把大型内部页面装入内存。
	data, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024+1))
	if err != nil || len(data) > 2*1024*1024 {
		return &Error{502, "hosting_invalid_control_response"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		status := http.StatusBadGateway
		if response.StatusCode == 404 || response.StatusCode == 409 || response.StatusCode == 429 {
			status = response.StatusCode
		}
		return &Error{status, "hosting_control_rejected"}
	}
	var envelope struct {
		Code json.RawMessage `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if common.Unmarshal(data, &envelope) != nil || string(envelope.Code) != "0" || len(envelope.Data) == 0 {
		return &Error{502, "hosting_invalid_control_response"}
	}
	if result == nil {
		return nil
	}
	if err := common.Unmarshal(envelope.Data, result); err != nil {
		return &Error{502, "hosting_invalid_control_response"}
	}
	return nil
}

// Authorization 只包含浏览器需要的授权 URL 和非秘密状态。
// PKCE、authorization code、原厂 token 不进入此 DTO。
type Authorization struct {
	StageID          string `json:"stage_id"`
	Status           string `json:"status"`
	AuthorizationURL string `json:"authorization_url"` // 短期授权地址只给发起会话显示。
	RedirectURI      string `json:"redirect_uri"`      // 仅展示原厂回调约定，Oak 不访问这个地址。
	ExpiresAtMS      int64  `json:"expires_at_ms"`     // 执行层时间单位为毫秒，本地持久字段使用秒。
}

// RemoteAccount 的内部 Key 只在控制面创建结果中出现。
// 控制器必须投影为 HostingAccount，禁止直接返回本结构。
type RemoteAccount struct {
	AccountRef          string       `json:"account_ref"`
	OwnerRef            string       `json:"owner_ref"`
	Provider            string       `json:"provider"`
	Alias               string       `json:"alias"`
	Status              string       `json:"status"`
	GroupID             uint64       `json:"group_id"`
	CredentialID        uint64       `json:"credential_id"`
	AccessKeyID         uint64       `json:"access_key_id"`
	IdentityFingerprint string       `json:"identity_fingerprint"` // 可信身份去重所需，不应展示给最终用户。
	EmailMask           string       `json:"email_mask"`
	InternalKey         string       `json:"internal_key"` // 只在创建幂等结果中接收，必须加密后持久化。
	Observation         *Observation `json:"observation,omitempty"`
}

// Observation 使用白名单投影，故意不包括原始 account_summary 邮箱。
// 数值保留指针语义：未知不能自动显示为零或无限。
type Observation struct {
	State              string               `json:"state"`
	Snapshot           *ObservationSnapshot `json:"snapshot"`
	ObservationVersion uint64               `json:"observation_version"` // 标识观察更新顺序，不代表额度新鲜。
	ObservedAtMS       *int64               `json:"observed_at_ms"`      // 缺失代表没有成功观察，不能使用本地当前时间替代。
	LastAttemptAtMS    *int64               `json:"last_attempt_at_ms"`  // 失败尝试时间与成功观察时间必须区分。
	LastErrorCode      string               `json:"last_error_code,omitempty"`
}

// ObservationSnapshot 只保留额度与套餐元信息，不对窗口或参考成本做人民币换算。
type ObservationSnapshot struct {
	PlanSummary struct {
		Name  string `json:"name"`
		Level string `json:"level"`
	} `json:"plan_summary"`
	QuotaWindows          []QuotaWindow       `json:"quota_windows"`
	Credits               *CreditsObservation `json:"credits,omitempty"`
	ResetCreditsAvailable *int64              `json:"reset_credits_available,omitempty"` // 未知与零张卡不同，不允许用默认零覆盖。
	ResetCredits          []struct {
		ExpiresAtMS *int64 `json:"expires_at_ms,omitempty"`
	} `json:"reset_credits,omitempty"`
	ResetCreditObservation *ResetCreditObservation `json:"reset_credit_observation,omitempty"` // 重置卡观察独立于主额度窗口状态。
}

// QuotaWindow 保留上游的窗口作用域和模型集合，不能跨账号相加百分比。
type QuotaWindow struct {
	ID            string   `json:"id"`
	Label         string   `json:"label"`
	Scope         string   `json:"scope"` // 窗口仅对声明的账号或模型范围生效。
	Unit          string   `json:"unit"`  // 原厂单位不得擅自换算成人民币余额。
	Used          *float64 `json:"used,omitempty"`
	Limit         *float64 `json:"limit,omitempty"`
	Remaining     *float64 `json:"remaining,omitempty"`
	Utilization   *float64 `json:"utilization,omitempty"` // 重叠窗口的百分比不能直接相加。
	ResetAtMS     *int64   `json:"reset_at_ms,omitempty"` // 窗口重置后旧的耗尽结论立即失效。
	WindowSeconds *int64   `json:"window_seconds,omitempty"`
	ModelIDs      []string `json:"model_ids,omitempty"` // 模型范围约束，不能把局部耗尽扩散至所有模型。
	State         string   `json:"state"`
}

// Models 与真实账号的受限模型目录一致，不混入公共池模型。
type Models struct {
	Models []struct {
		ID string `json:"id"`
	} `json:"models"`
}

// ProbeResult 仅包含调用结论，不返回生成正文或测试凭据。
type ProbeResult struct {
	Outcome    string `json:"outcome"` // passed 之外的结论不能激活未验证账号。
	Model      string `json:"model"`
	Protocol   string `json:"protocol"`
	LatencyMS  int64  `json:"latency_ms"`
	Reason     string `json:"reason,omitempty"`
	TestedAtMS int64  `json:"tested_at_ms"`
}

// ResetResult 与消费操作的持久状态对齐，观测失败不覆盖消费成功。
type ResetResult struct {
	Status             string       `json:"status"`
	WindowsReset       int          `json:"windows_reset"`
	RedeemedAtMS       *int64       `json:"redeemed_at_ms,omitempty"`
	Observation        *Observation `json:"observation,omitempty"`
	ObservationPending bool         `json:"observation_pending,omitempty"` // 消费成功后观测未完成，不代表应再次消费。
	Replayed           bool         `json:"replayed"`                      // 执行层返回原操作结果，不能据此生成新操作。
}

// cryptKey 强制显式持久密钥，拒绝应用启动时随机生成的默认 CryptoSecret。
// AAD 绑定远端账号引用，数据库内交换两行密文也不能交换访问权限。
// 当前应用默认 CryptoSecret 可能随进程重启随机生成，不能用于持久凭据。
// 因此直接检查显式部署环境，而不接受运行时的随机兜底值。
// 派生密钥包含用途前缀，避免与其他加密用途直接复用相同 AES 密钥。
// AAD 绑定账号引用，使不同数据库行之间的密文置换无法解密。
// 错误不会返回密钥长度以外的任何秘密内容。
// 部署密钥变更需要重新接入或受控迁移，不能静默使用旧密文。
func cryptKey() (cipher.AEAD, error) {
	secret := os.Getenv("CRYPTO_SECRET")
	if len(secret) < 32 {
		return nil, &Error{503, "hosting_encryption_not_configured"}
	}
	key := sha256.Sum256([]byte("oak-hosting-v1\x00" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// EncryptKey 只加密内部受限 Key；原厂 OAuth token 不由 Oak 保存。
// 只接受非空账号引用与受限执行 Key，空值不能变成合法凭据。
// 每次加密都使用新的随机 nonce，相同明文不会产生可关联的相同密文。
// nonce 随密文存储，它不是秘密，但长度必须在解密前检查。
// 持久字段采用无填充 Base64，避免数据库字符集影响二进制内容。
// 本函数不加密或持有原厂 OAuth token，原厂凭据仍归执行层管理。
// 加密失败会阻止远端 activate，避免产生本地不可管理的启用账号。
func EncryptKey(accountRef, plaintext string) (string, error) {
	if plaintext == "" || accountRef == "" {
		return "", errors.New("missing hosting credential")
	}
	aead, err := cryptKey()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := aead.Seal(nonce, nonce, []byte(plaintext), []byte(accountRef))
	return base64.RawStdEncoding.EncodeToString(ciphertext), nil
}

// Route 是可信调度上下文，只能留在服务端，不可序列化给浏览器。
type Route struct {
	BaseURL     string       `json:"-"`
	AccessKey   string       `json:"-"`
	Models      []string     `json:"-"`
	AccountID   int          `json:"-"`
	Paused      bool         `json:"-"`
	State       string       `json:"-"`
	Observation *Observation `json:"-"`
}

// ResolveRoute 每次重新核对本地所有者和停用状态，不依赖列表缓存授权。
// 不可用账号保留明确状态供调度跳过；解密或控制面失败不能伪装成额度耗尽。
// 每次首先重新查本地归属和状态，不相信客户端携带的账号信息。
// 暂停或生命周期不可用时只返回状态，不读取密钥或访问执行层。
// connected 仅供目录展示，实际调度必须要求 active 才能发出数据请求。
// 解密使用当前持久密钥和原账号 AAD，损坏或被交换的密文显式拒绝。
// 内部 Key 只放在请求内存中，不写入用户响应、审计或访问日志。
// 模型目录从该账号的受限执行组读取，不用平台公共模型补齐。
// 额度只读执行层已有缓存，不在每次推理前主动访问原厂刷新接口。
// 额度新鲜度和窗口适用范围由调度器判断，此处不推断耗尽。
// 控制面失败是未知执行能力，不能伪装成额度为零后自动扣钱包。
// 返回 URL 已由部署边界校验，用户不能用模型名改变请求目标。
// 模型列表和账号状态是一次路由准备的快照，不改写用户策略。
func ResolveRoute(ctx context.Context, userID, accountID int) (*Route, error) {
	account, err := model.GetHostingAccount(userID, accountID)
	if err != nil {
		return nil, err
	}
	route := &Route{AccountID: account.ID, State: account.Status, Paused: account.Paused}
	if account.Paused || (account.Status != "active" && account.Status != "connected") {
		return route, nil
	}
	client, err := NewClient()
	if err != nil {
		return nil, err
	}
	aead, err := cryptKey()
	if err != nil {
		return nil, err
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(account.EncryptedKey)
	if err != nil || len(ciphertext) < aead.NonceSize() {
		return nil, &Error{503, "hosting_credential_unavailable"}
	}
	plaintext, err := aead.Open(nil, ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():], []byte(account.RemoteRef))
	if err != nil {
		return nil, &Error{503, "hosting_credential_unavailable"}
	}
	var models Models
	if err := client.Call(ctx, http.MethodGet, "/accounts/"+url.PathEscape(account.RemoteRef)+"/models", userID, nil, "", &models); err != nil {
		return nil, err
	}
	route.BaseURL, route.AccessKey = client.BaseURL, string(plaintext)
	for _, item := range models.Models {
		route.Models = append(route.Models, item.ID)
	}
	// 读取执行层缓存中的最新观测，不让用户手动刷新决定耗尽信息是否更新。
	// 此 GET 不主动刷新上游；新鲜度仍由调度器逐窗口判断。
	if err := client.Call(ctx, http.MethodGet, "/accounts/"+url.PathEscape(account.RemoteRef)+"/observation", userID, nil, "", &route.Observation); err != nil {
		return nil, err
	}
	return route, nil
}

// CreditsObservation 只表示原厂账户余额事实，不换算为平台货币。
// 指针区分未知和零；原厂未披露字段不会由平台推算补齐。
type CreditsObservation struct {
	Balance      *string `json:"balance,omitempty"`     // 保留原厂精度与格式，不转成平台计费单位。
	HasCredits   *bool   `json:"has_credits,omitempty"` // 缺失是未知，false 才是明确没有余额。
	Unlimited    *bool   `json:"unlimited,omitempty"`   // 必须由原厂明确给出，未知不能显示为无限。
	ObservedAtMS *int64  `json:"observed_at_ms,omitempty"`
}

// ResetCreditObservation 独立标识重置卡数据新鲜度。
// 主额度 fresh 不能证明重置次数仍新鲜，界面必须分别展示。
type ResetCreditObservation struct {
	State        string `json:"state"`
	ObservedAtMS *int64 `json:"observed_at_ms,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
}
