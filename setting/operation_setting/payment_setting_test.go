package operation_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 管理员读取配置后看到空密钥，再保存名称不应清空真实凭据。
// 使用真实全局配置作为旧值，保护服务端空值合并规则。
// 清理函数恢复原渠道数组，不能污染其他配置测试。
// fixture包含完整的启用支付宝渠道，确保保存走实际校验。
// 提交空私钥和公钥模拟前端遮罩后的编辑值。
// 名称变更验证普通字段仍会被更新，不能简单忽略整个请求。
// 返回JSON重新解析为真实渠道结构，不断言内部临时映射。
// 私钥与平台公钥都应保留旧值，不能只保护其中一个字段。
// 测试值是明显占位的内存fixture，不包含真实商户密钥。
// 本用例只保护配置合并，不执行支付请求或数据库持久化。
// 归一化与校验由生产函数完成，测试不复制其白名单逻辑。
// 保存成功不能依赖浏览器重新提交已隐藏的凭据。
func TestPrepareOfficialPaymentChannelsUpdatePreservesRedactedSecrets(t *testing.T) {
	original := paymentSetting.OfficialChannels
	t.Cleanup(func() { paymentSetting.OfficialChannels = original })
	paymentSetting.OfficialChannels = []OfficialPaymentChannel{
		{
			ID:              "alipay-main",
			Name:            "Alipay",
			ChannelType:     PaymentChannelAlipay,
			InteractionMode: PaymentInteractionPage,
			Enabled:         true,
			Config: map[string]string{
				"app_id":            "2026000000000000",
				"private_key":       "stored-private-key",
				"alipay_public_key": "stored-public-key",
				"gateway_url":       "https://openapi.alipay.com/gateway.do",
				"sign_type":         "RSA2",
			},
		},
	}

	raw := `[{
		"id":"alipay-main",
		"name":"Alipay Direct",
		"channel_type":"alipay",
		"interaction_mode":"page",
		"enabled":true,
		"config":{
			"app_id":"2026000000000000",
			"private_key":"",
			"alipay_public_key":"",
			"gateway_url":"https://openapi.alipay.com/gateway.do",
			"sign_type":"RSA2"
		}
	}]`
	prepared, err := PrepareOfficialPaymentChannelsUpdate(raw)
	require.NoError(t, err)

	var channels []OfficialPaymentChannel
	require.NoError(t, common.UnmarshalJsonStr(prepared, &channels))
	require.Len(t, channels, 1)
	assert.Equal(t, "stored-private-key", channels[0].Config["private_key"])
	assert.Equal(t, "stored-public-key", channels[0].Config["alipay_public_key"])
	assert.Equal(t, "Alipay Direct", channels[0].Name)
}

// 配置读取必须同时遮罩商户私钥、APIv3密钥和平台公钥。
// fixture采用微信渠道，覆盖与支付宝不同的秘密字段集合。
// 返回JSON使用真实结构解析，避免字符串搜索遗漏嵌套字段。
// 三个私密字段都必须为空，不能仅替换顶层同名字段。
// 公钥ID保留供管理员确认配置身份，它不等于公钥正文。
// 已配置标记只列字段名，不含原文、部分字符或摘要。
// 使用ElementsMatch避免把字段列举顺序误当业务契约。
// 测试不把完整遮罩前配置打印到失败日志。
// 明文密钥只存在于测试fixture，不写仓库配置或环境变量。
// 前端保存空字段的保留行为由上一用例单独保护。
// 遮罩函数不得改变渠道类型与交互方式，后续编辑仍能定位原渠道。
// 本用例验证的是数据输出边界，不以UI密码框隐藏代替后端遮罩。
func TestRedactOfficialPaymentChannelsHidesCredentials(t *testing.T) {
	raw := `[{"id":"wechat-main","name":"WeChat","channel_type":"wechat","interaction_mode":"qr","enabled":true,"config":{"appid":"wx123","mchid":"1900000001","merchant_serial_no":"SERIAL","merchant_private_key":"private","api_v3_key":"12345678901234567890123456789012","wechatpay_public_key_id":"PUB_KEY_ID_TEST","wechatpay_public_key":"public","h5_type":"WAP"}}]`
	redacted := RedactOfficialPaymentChannels(raw)

	var channels []OfficialPaymentChannel
	require.NoError(t, common.UnmarshalJsonStr(redacted, &channels))
	require.Len(t, channels, 1)
	assert.Empty(t, channels[0].Config["merchant_private_key"])
	assert.Empty(t, channels[0].Config["api_v3_key"])
	assert.Empty(t, channels[0].Config["wechatpay_public_key"])
	assert.Equal(t, "PUB_KEY_ID_TEST", channels[0].Config["wechatpay_public_key_id"])
	assert.ElementsMatch(t, []string{"merchant_private_key", "api_v3_key", "wechatpay_public_key"}, channels[0].ConfiguredSecrets)
}

// 微信渠道当前只实现Native扫码和H5跳转。
// 使用支付宝专属page交互应明确拒绝，不能猜测为微信H5。
// fixture不启用渠道，避免必填商户凭据掩盖交互类型错误。
// 错误必须指向交互方式，便于管理员定位实际配置问题。
// 直接调用生产校验，不用表单端枚举替后端安全边界自证。
// 失败不应发起网络，也不应创建任何付款订单。
// 该测试防止前端新增选项后后端默默接受尚未实现的模式。
// 采用明确错误片段而非全量文本，允许国际化参数演进。
func TestValidateOfficialPaymentChannelRejectsUnsupportedMode(t *testing.T) {
	channel := OfficialPaymentChannel{
		ID:              "wechat-main",
		Name:            "WeChat",
		ChannelType:     PaymentChannelWechat,
		InteractionMode: PaymentInteractionPage,
		Config:          map[string]string{"h5_type": "WAP"},
	}

	err := ValidateOfficialPaymentChannel(channel)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "interaction mode")
}
