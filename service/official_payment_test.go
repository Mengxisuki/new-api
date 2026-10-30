package service

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 通过真实RSA密钥和真实支付宝签名串验证通知协议。
// 测试不模拟验签函数，签名结果必须被生产实现接受。
// 生成密钥只用于测试进程，不读取部署中的商户私钥。
// 应用ID选固定测试值，避免依赖真实商户或外部网络。
// 表单包含订单号、上游交易号、成功状态和两位金额。
// 签名使用RSA2，与官方默认SHA256方式一致。
// 先验证成功路径，确认返回平台订单号未被重写。
// 金额断言使用decimal文本，避免浮点近似掩盖精度问题。
// 成功状态从通知提取，而非测试直接替换返回对象。
// 随后只修改应用ID，原签名和其他交易信息不变。
// 跨应用通知必须被拒绝，避免同平台公钥下错误认领订单。
// 错误类别使用公共哨兵断言，不依赖内部报错完整文本。
// 此测试只保护支付来源与协议解析，不访问模型数据库。
// 通知成功不等于权益发放，控制器和模型另有结算回归。
// 生产签名串排序实现被实际调用，字段顺序不影响验签结果。
// 所有资源为内存数据，测试结束无需删除文件或撤销交易。
func TestVerifyOfficialAlipayCallback(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	privatePEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: mustMarshalPKCS8(t, privateKey)}))
	publicPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: mustMarshalPublicKey(t, &privateKey.PublicKey)}))
	// 渠道fixture使用生产结构，不替换SDK读取的配置对象。
	channel := operation_setting.OfficialPaymentChannel{
		ID:              "alipay-main",
		Name:            "Alipay",
		ChannelType:     operation_setting.PaymentChannelAlipay,
		InteractionMode: operation_setting.PaymentInteractionPage,
		Enabled:         true,
		Config: map[string]string{
			"app_id":            "2026000000000000",
			"private_key":       privatePEM,
			"alipay_public_key": publicPEM,
			"gateway_url":       "https://openapi.alipay.com/gateway.do",
			"sign_type":         "RSA2",
		},
	}
	// 表单字段是上游协议样例，不从被测返回值反向生成。
	form := url.Values{
		"app_id":       {"2026000000000000"},
		"out_trade_no": {"USR1NO123"},
		"trade_no":     {"202608040001"},
		"trade_status": {"TRADE_SUCCESS"},
		"total_amount": {"10.50"},
		"sign_type":    {"RSA2"},
	}
	signature, err := signOfficialAlipayContent(buildAlipayCallbackSignContent(form), privatePEM, "RSA2")
	require.NoError(t, err)
	form.Set("sign", signature) // 签名生成后保持表单原文不变，直到失败场景刻意篡改。

	callback, err := VerifyOfficialAlipayCallback(channel, form)
	require.NoError(t, err)
	assert.Equal(t, "USR1NO123", callback.TradeNo)
	assert.Equal(t, "10.5", callback.Amount.String())
	assert.Equal(t, "TRADE_SUCCESS", callback.Status)

	form.Set("app_id", "other-app")
	_, err = VerifyOfficialAlipayCallback(channel, form)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrOfficialPaymentSignature)
}

// 覆盖微信APIv3固定公钥验签和AES256-GCM资源解密。
// 测试使用真实RSA签名，不模拟SDK或通知解析器。
// APIv3密钥为明确测试数据，仅用于生成本次通知密文。
// 同一测试私钥的公钥模拟微信平台公钥，公钥ID使用官方前缀。
// 交易资源含应用ID和商户号，检验签名之外的账户归属。
// 金额采用整数分，生产解析结果应准确换算为元。
// 密文使用十二字节nonce与associated_data，遵循GCM格式。
// 通知外层保留算法、资源类型和成功事件，检验完整解析路径。
// 原始JSON报文字节参与签名，签完后不重新序列化报文。
// 时间戳使用当前测试时刻，避免SDK新鲜度校验误拒绝固定历史数据。
// 签名串严格包含协议要求的换行，不能用字符串拼接遗漏尾换行。
// HTTP头显式提供平台序列ID、时间戳、nonce与签名算法。
// 成功断言核对平台订单号，避免把上游交易号误当本地订单。
// 同时核对上游交易号，保证审计定位信息保留。
// 成功金额为10.5元，原始数据1050分只经过一次换算。
// 返回状态必须是SUCCESS，控制器才会进入权益结算。
// 验签后修改配置商户号，原始报文签名仍合法。
// 这一步专门防止“签名合法即允许任意商户交易”的错误。
// 商户不匹配应归为签名归属错误，不允许继续入账。
// 恢复商户后再修改公钥ID，验证密钥选择也受约束。
// 不能只使用公钥内容而忽略平台序列ID。
// 测试不访问微信网络，不创建真实订单或付款。
// 加密和签名都使用标准库，与生产SDK保持独立实现。
// 预期值来自显式交易fixture，不复制生产业务规则计算结果。
// 错误链使用ErrorIs，允许内部上下文信息变化而保持契约稳定。
// 本测试曾在缺少商户绑定时失败，补齐校验后转为通过。
// 回调原文与密文不写测试日志，避免以后误把真实样本泄漏。
// 测试只验证通知可信性，额度与订阅幂等由真实数据库测试覆盖。
func TestVerifyOfficialWechatCallbackWithPublicKey(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	publicPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: mustMarshalPublicKey(t, &privateKey.PublicKey)}))
	apiV3Key := "12345678901234567890123456789012"
	publicKeyID := "PUB_KEY_ID_TEST_001"
	// 渠道fixture使用生产结构，不替换SDK读取的配置对象。
	channel := operation_setting.OfficialPaymentChannel{
		ID:              "wechat-main",
		Name:            "WeChat",
		ChannelType:     operation_setting.PaymentChannelWechat,
		InteractionMode: operation_setting.PaymentInteractionQR,
		Enabled:         true,
		Config: map[string]string{
			"api_v3_key":              apiV3Key,
			"appid":                   "wx-test-app",
			"mchid":                   "test-merchant",
			"wechatpay_public_key_id": publicKeyID,
			"wechatpay_public_key":    publicPEM,
		},
	}
	transaction := map[string]any{
		"out_trade_no":   "USR1NO123",
		"appid":          "wx-test-app",
		"mchid":          "test-merchant",
		"transaction_id": "4200000000001",
		"trade_state":    "SUCCESS",
		"amount": map[string]any{
			"total":    1050,
			"currency": "CNY",
		},
	}
	// 明文先编码一次；密文验签保护的是外层通知原始字节。
	plaintext, err := common.Marshal(transaction)
	require.NoError(t, err)
	block, err := aes.NewCipher([]byte(apiV3Key))
	require.NoError(t, err)
	aead, err := cipher.NewGCM(block)
	require.NoError(t, err)
	nonce := "0123456789ab" // 固定十二字节测试nonce，避免无关随机性干扰协议复现。
	associatedData := "transaction"
	// 独立使用标准库生成微信资源密文，生产解密仍由真实SDK完成。
	ciphertext := aead.Seal(nil, []byte(nonce), plaintext, []byte(associatedData))
	body, err := common.Marshal(map[string]any{
		"id":            "notify-001",
		"create_time":   "2026-08-05T10:00:00+08:00",
		"event_type":    "TRANSACTION.SUCCESS",
		"resource_type": "encrypt-resource",
		"resource": map[string]any{
			"algorithm":       "AEAD_AES_256_GCM",
			"ciphertext":      base64.StdEncoding.EncodeToString(ciphertext),
			"nonce":           nonce,
			"associated_data": associatedData,
		},
	})
	require.NoError(t, err)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	signatureMessage := timestamp + "\n" + nonce + "\n" + string(body) + "\n"
	digest := sha256.Sum256([]byte(signatureMessage))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	require.NoError(t, err)
	// 大小写通过HTTP Header规则处理，字段值按协议精确签署。
	headers := http.Header{
		"Wechatpay-Timestamp":      {timestamp},
		"Wechatpay-Nonce":          {nonce},
		"Wechatpay-Signature":      {base64.StdEncoding.EncodeToString(signature)},
		"Wechatpay-Serial":         {publicKeyID},
		"Wechatpay-Signature-Type": {"WECHATPAY2-SHA256-RSA2048"},
	}

	callback, err := VerifyOfficialWechatCallback(context.Background(), channel, headers, body)
	require.NoError(t, err)
	assert.Equal(t, "USR1NO123", callback.TradeNo)
	assert.Equal(t, "4200000000001", callback.ProviderRef)
	assert.Equal(t, "10.5", callback.Amount.String())
	assert.Equal(t, "SUCCESS", callback.Status)

	// 签名合法也必须绑定当前商户，不能跨渠道认领别人的交易。
	channel.Config["mchid"] = "other-merchant"
	_, err = VerifyOfficialWechatCallback(context.Background(), channel, headers, body)
	require.ErrorIs(t, err, ErrOfficialPaymentSignature)
	channel.Config["mchid"] = "test-merchant" // 恢复商户再单独验证公钥ID不匹配。

	headers.Set("Wechatpay-Serial", "PUB_KEY_ID_OTHER")
	_, err = VerifyOfficialWechatCallback(context.Background(), channel, headers, body)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrOfficialPaymentSignature)
}

// 将测试私钥编码为标准PKCS8，供真实生产私钥解析器读取。
// 编解码本身走标准库，测试不调用生产私钥解析辅助函数造结果。
// 错误通过require终止当前测试，不能返回空PEM继续验签。
// 该函数只用于测试fixture，不作为新的业务加密封装。
// 编码结果仅保存在内存，不创建密钥文件。
// 私钥来源是当前测试生成的临时RSA密钥。
func mustMarshalPKCS8(t *testing.T, key *rsa.PrivateKey) []byte {
	t.Helper()
	data, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return data
}

// 将临时RSA公钥编码为PKIX，用于模拟厂商提供的验签资料。
// 公钥与签名私钥成对，但不会从生产配置读取或反推出密钥。
// 编码失败应立即终止测试，不能以空公钥误测其他错误分支。
// 只抽取重复的fixture编码，保留验签协议本身在测试中显式可见。
// 返回字节不包含任何部署环境的商户信息。
// 生产解析必须独立识别该标准公钥格式。
func mustMarshalPublicKey(t *testing.T, key *rsa.PublicKey) []byte {
	t.Helper()
	data, err := x509.MarshalPKIXPublicKey(key)
	require.NoError(t, err)
	return data
}

// 付款人IP是上游风控输入，非法地址必须显式失败。
// 旧source会回退到127.0.0.1，掩盖代理配置或请求来源错误。
// 使用真实归一化函数验证输入契约，不模拟net包解析结果。
// 带端口IPv6必须通过SplitHostPort，不能按冒号机械拆分。
// 合法回环地址可以原样使用，但不能作为无效输入的替代值。
// 测试不触发微信网络，全部地址都是明确的本地fixture。
func TestNormalizeOfficialPaymentIPRejectsInvalidAddress(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{"192.0.2.5", "192.0.2.5"},           // 标准文档IPv4地址按原协议保留。
		{"[2001:db8::1]:443", "2001:db8::1"}, // IPv6端口必须移除且地址保持有效。
		{"bad-ip", ""},                       // 无效文本不能伪装成默认付款人。
		{"", ""},                             // 缺失输入同样不应静默补回环地址。
	} {
		t.Run(tt.input, func(t *testing.T) {
			got, err := normalizeOfficialPaymentIP(tt.input)
			if tt.want == "" { // 无效输入的输出必须为空，不泄漏部分解析地址。
				require.ErrorIs(t, err, ErrOfficialPaymentConfig)
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err) // 合法IP仍需保持原有成功行为。
			assert.Equal(t, tt.want, got)
		})
	}
}
