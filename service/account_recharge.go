package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/mail"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// 敏感证明与挂靠授权分开，
// 绑定订单版本后不可跨单重放。
const VerificationScopeRecharge = "recharge.manage"

// 读取和退款都需要单次证明，
// 业务入口另行检查对应权限。
type RechargeVerificationContext struct {
	OrderID int    `json:"order_id"` // 正数订单编号，不接受通配范围。
	Version int    `json:"version"`  // 领取、转派、资料更新会改变版本。
	Action  string `json:"action"`   // reveal/refund/refund_confirm 白名单。
}

// 私密资料只能在提交与受保护读取之间短暂出现于内存。
// 不将结构用于订单列表、普通详情、审计或请求日志。
type RechargeAccountInfo struct {
	Email   string `json:"email"`
	Session string `json:"session"`
}

// 对外可读错误限定为固定业务文案，控制器不得透传解析器或加密库原始错误。
// 邮箱冲突应由用户检查账号；密钥缺失则需要管理员修复配置。
var (
	ErrRechargeAccountInfo   = errors.New("Invalid recharge account information")
	ErrRechargeEmailMismatch = errors.New("Account email does not match the session")
	ErrRechargeEncryption    = errors.New("Recharge encryption is not configured")
)

// 独立密钥必须显式配置，
// 不能复用session签名密钥或弱默认值。
// 缺失时停止接收订单资料，
// 避免明文落库或产生不可解密的记录。
// 密钥使用标准 Base64 编码的32字节随机数，
// 运维需要持久备份。
func rechargeCipher() (cipher.AEAD, error) {
	key, err := base64.StdEncoding.DecodeString(os.Getenv("OAK_RECHARGE_DATA_KEY"))
	if err != nil || len(key) != 32 {
		return nil, ErrRechargeEncryption
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// 下单前检查部署具备接收资料的能力；
// 不向浏览器返回密钥。
func RechargeConfigured() bool { _, err := rechargeCipher(); return err == nil }

// 输入边界只验证格式与邮箱一致性，
// 不访问或验证用户的ChatGPT 会话。
// 原始 JSON按原样保存，
// 不能因为解析而丢失上游需要的字段。
// 错误固定文本，
// 禁止将解析器原文或用户提交值输出到日志。
// 邮箱只允许纯地址，
// 拒绝带显示名的输入，
// 避免两个字段产生歧义。
// 会话必须是非空 JSON对象；
// 错误页面和空对象不能入队办理。
// 长度限制与控制器请求体上限共同约束内存占用。
// 包含用户邮箱时做不区分大小写的核对，
// 账号不一致直接拒绝。
// 没有邮箱字段并不伪造身份，
// 真实可办理性仍由业务员核实。
// 此处不调用原厂接口，
// 也不保证会话未过期或已成功充值。
// 认证加密同时保护内容机密性及完整性，
// 关联数据防止跨订单调包。
// 数据库只保存 Base64密文和邮箱掩码，
// 解析错误也不会带回原文。
// 返回的掩码只供人工区分订单，
// 不能据此跳过敏感验证。
// 失败没有部分成功结果，
// 调用者不得保存其中任一空返回值。
func EncryptRechargeInfo(tradeNo string, info RechargeAccountInfo) (string, string, error) {
	info.Email = strings.TrimSpace(info.Email)
	address, err := mail.ParseAddress(info.Email)
	if err != nil || address.Address != info.Email || len(info.Email) > 254 || len(info.Session) == 0 || len(info.Session) > 64*1024 {
		return "", "", ErrRechargeAccountInfo
	}
	var session map[string]any
	if common.Unmarshal([]byte(info.Session), &session) != nil || len(session) == 0 {
		return "", "", ErrRechargeAccountInfo
	}
	// 若响应中提供邮箱就核对，
	// 避免人工将A 账号凭据用于 B邮箱。
	if user, ok := session["user"].(map[string]any); ok {
		if email, ok := user["email"].(string); ok && !strings.EqualFold(email, info.Email) {
			return "", "", ErrRechargeEmailMismatch
		}
	}
	aead, err := rechargeCipher()
	if err != nil {
		return "", "", err
	}
	plain, err := common.Marshal(info)
	if err != nil {
		return "", "", err
	}
	// 每次提交使用新nonce；
	// 订单号作为关联数据，
	// 调换密文无法解密。
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", "", err
	}
	encrypted := aead.Seal(nonce, nonce, plain, []byte(tradeNo))
	_, domain, _ := strings.Cut(info.Email, "@")
	mask := string([]rune(info.Email)[0]) + "***@" + domain
	return base64.StdEncoding.EncodeToString(encrypted), mask, nil
}

// 调用前必须完成授权和读取审计；
// 密文篡改统一失败。
// 返回资料仅供 no-store响应，
// 不写入普通订单结构。
// 输入来自受保护模型读取，
// 不能提供任意密文解密公共接口。
// 先核对长度再截取nonce，
// 避免损坏记录触发越界。
// 认证标签错误与字段损坏都视为资料不可用，
// 不向用户披露密钥细节。
// 订单号必须采用数据库冻结值，
// 不能由浏览器指定关联数据。
// 环境密钥变更不会尝试弱默认密钥；
// 运维须先处理现存待办资料。
// 解密后的 JSON只发送给当前授权读取请求，
// 不能回写到订单字段。
func DecryptRechargeInfo(tradeNo, secret string) (RechargeAccountInfo, error) {
	var info RechargeAccountInfo
	aead, err := rechargeCipher()
	if err != nil {
		return info, err
	}
	encrypted, err := base64.StdEncoding.DecodeString(secret)
	if err != nil || len(encrypted) < aead.NonceSize() {
		return info, errors.New("Recharge account information is unavailable")
	}
	plain, err := aead.Open(nil, encrypted[:aead.NonceSize()], encrypted[aead.NonceSize():], []byte(tradeNo))
	if err != nil {
		return info, errors.New("Recharge account information is unavailable")
	}
	err = common.Unmarshal(plain, &info)
	return info, err
}
