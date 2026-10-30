package operation_setting

import (
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const (
	PaymentProviderOfficial = "official" // 前端渠道分派标识，不是具体到账厂商。
	PaymentChannelAlipay    = "alipay"   // 支付宝与微信配置字段隔离。
	PaymentChannelWechat    = "wechat"   // 微信支付不等于微信登录配置。

	PaymentInteractionQR       = "qr"       // 二维码内容交给前端本地生成二维码图。
	PaymentInteractionPage     = "page"     // 页面付款是支付宝专用交互。
	PaymentInteractionWAP      = "wap"      // 支付宝移动网页不使用微信H5协议。
	PaymentInteractionRedirect = "redirect" // 微信H5跳转仍等待异步到账。
)

// 允许的字符可安全进入回调路径及五十字符的订单渠道列。
var officialPaymentChannelIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,50}$`)

type OfficialPaymentChannel struct {
	ID                string            `json:"id"`                           // 与订单PaymentMethod关联，不能按显示名称替代。
	Name              string            `json:"name"`                         // 用户可见标签，不用于回调认领。
	Icon              string            `json:"icon,omitempty"`               // 展示字段不影响付款路由。
	ChannelType       string            `json:"channel_type"`                 // 决定SDK和验签协议。
	InteractionMode   string            `json:"interaction_mode"`             // 后端校验已实现的交互集合。
	MinTopUp          int64             `json:"min_topup,omitempty"`          // 仅限制钱包充值，订阅按套餐价单独付款。
	Enabled           bool              `json:"enabled"`                      // 停用只关闭新付款，不阻断历史通知验证。
	SortOrder         int               `json:"sort_order,omitempty"`         // 排序不改变订单身份和网关匹配。
	Config            map[string]string `json:"config"`                       // 含商户秘密，只能由受保护设置读取遮罩版本。
	ConfiguredSecrets []string          `json:"configured_secrets,omitempty"` // 服务端派生标记，不能信任客户端提交。
}

type PaymentSetting struct {
	AmountOptions    []int                    `json:"amount_options"`
	AmountDiscount   map[int]float64          `json:"amount_discount"`   // 充值金额对应的折扣，例如 100 元 0.9 表示 100 元充值享受 9 折优惠
	OfficialChannels []OfficialPaymentChannel `json:"official_channels"` // 复用现有系统配置，无单独支付配置表。

	ComplianceConfirmed    bool   `json:"compliance_confirmed"`
	ComplianceTermsVersion string `json:"compliance_terms_version"`
	ComplianceConfirmedAt  int64  `json:"compliance_confirmed_at"`
	ComplianceConfirmedBy  int    `json:"compliance_confirmed_by"`
	ComplianceConfirmedIP  string `json:"compliance_confirmed_ip"`
}

const CurrentComplianceTermsVersion = "v1"

// 默认配置
var paymentSetting = PaymentSetting{
	AmountOptions:    []int{10, 20, 50, 100, 200, 500},
	AmountDiscount:   map[int]float64{},
	OfficialChannels: []OfficialPaymentChannel{},
}

// 官方渠道随现有payment_setting注册，加载与保存使用同一配置机制。
func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("payment_setting", &paymentSetting)
}

// 后端获取完整配置；禁止把整个返回对象直接公开为用户支付信息。
func GetPaymentSetting() *PaymentSetting {
	return &paymentSetting
}

// 继续复用既有合规确认版本规则，不为新渠道另增授权开关。
func IsPaymentComplianceConfirmed() bool {
	return paymentSetting.ComplianceConfirmed &&
		paymentSetting.ComplianceTermsVersion == CurrentComplianceTermsVersion
}

// 按稳定渠道ID取配置，不按用户显示名称匹配。
// activeOnly用于新下单和重试，停用渠道仍可验证历史付款通知。
// 回调不因渠道停用丢弃已成功的交易，因此由调用者明确选择模式。
// 配置必须完整有效；返回副本避免下单服务修改全局映射。
// 无匹配时返回明确布尔值，调用者不能回退为另一支付渠道。
// 副本中的私密配置只在后端使用，禁止整体输出给用户端。
// 渠道ID会持久化到订单PaymentMethod，后续回调必须完全一致。
func GetOfficialPaymentChannel(id string, activeOnly bool) (*OfficialPaymentChannel, bool) {
	id = strings.TrimSpace(id)
	for _, channel := range paymentSetting.OfficialChannels {
		if channel.ID != id || (activeOnly && !channel.Enabled) {
			continue
		}
		if err := ValidateOfficialPaymentChannel(channel); err != nil {
			return nil, false
		}
		copy := channel
		copy.Config = clonePaymentConfig(channel.Config) // 隔离可变映射，防止调用者污染全局配置。
		return &copy, true
	}
	return nil, false
}

// 列出能用于新付款的渠道，停用或缺失配置的渠道不开放购买。
// 这里只返回后端对象，用户接口须再投影为名称、类型等展示字段。
// 按sort_order稳定排序，保留相同排序值的配置顺序。
// 公开支付方式不得携带Config或ConfiguredSecrets内部字段。
// 返回映射独立复制，避免后续编辑污染原始配置。
// 管理员下架渠道只影响新付款，不撤销已存在的付款通知验证。
// 渠道验证失败不会提供一个可点却无法付款的入口。
func GetAvailableOfficialPaymentChannels() []OfficialPaymentChannel {
	channels := make([]OfficialPaymentChannel, 0, len(paymentSetting.OfficialChannels))
	for _, channel := range paymentSetting.OfficialChannels {
		if !channel.Enabled || ValidateOfficialPaymentChannel(channel) != nil {
			continue
		}
		copy := channel
		copy.Config = clonePaymentConfig(channel.Config) // 隔离可变映射，防止调用者污染全局配置。
		copy.ConfiguredSecrets = nil                     // 普通支付方式列表没有密钥编辑语义。
		channels = append(channels, copy)
	}
	// 同序号渠道保持原配置顺序，避免用户付款按钮随机换位。
	sort.SliceStable(channels, func(i, j int) bool {
		return channels[i].SortOrder < channels[j].SortOrder
	})
	return channels
}

// 通用设置入口保存官方渠道前使用的唯一规范化与校验路径。
// 输入必须是JSON数组，每个ID在同一份配置里只能出现一次。
// 先保存旧渠道快照，空私密字段代表保留原已配置值。
// 只在ID和渠道类型都相同的情况下合并私密字段。
// 切换支付宝和微信类型时不能沿用另一类商户密钥。
// 删除渠道通过从数组移除表达，不将空密钥误解为删除。
// ConfiguredSecrets由服务端计算，客户端提交的标记不可信。
// 未知配置字段会被归一化器清除，避免意外保存任意秘密资料。
// 启用中的渠道必须满足其交互方式所需必填项。
// 所有渠道先验证完才返回完整JSON，调用者据此一次持久化。
// 任何一项失败都终止保存，不部分更新前面的渠道。
// 错误返回可翻译的英文消息键，字段名作为参数保留协议标识。
// 本函数不直接写数据库，因此配置持久化仍遵循现有设置流程。
// 返回的JSON含服务端合并后的私密字段，只能用于内部保存。
// 读取接口必须走专用遮罩，不能把此处返回值原样发给浏览器。
func PrepareOfficialPaymentChannelsUpdate(raw string) (string, error) {
	var channels []OfficialPaymentChannel
	if err := common.UnmarshalJsonStr(raw, &channels); err != nil {
		return "", common.NewMessage("Official payment channels must be a valid JSON array")
	}

	existing := make(map[string]OfficialPaymentChannel, len(paymentSetting.OfficialChannels))
	for _, channel := range paymentSetting.OfficialChannels {
		existing[channel.ID] = channel
	}

	seen := make(map[string]struct{}, len(channels))
	// 按数组逐项处理，所有渠道验证完成后才生成可持久化配置。
	for i := range channels {
		normalizeOfficialPaymentChannel(&channels[i])
		if _, ok := seen[channels[i].ID]; ok {
			return "", common.NewMessage("Duplicate official payment channel ID: {{id}}", map[string]any{"id": channels[i].ID})
		}
		seen[channels[i].ID] = struct{}{}

		if previous, ok := existing[channels[i].ID]; ok && previous.ChannelType == channels[i].ChannelType {
			for _, key := range officialPaymentSecretKeys(channels[i].ChannelType) {
				if strings.TrimSpace(channels[i].Config[key]) == "" {
					channels[i].Config[key] = previous.Config[key]
				}
			}
		}
		channels[i].ConfiguredSecrets = nil // 避免客户端伪造“已配置”状态进入存储。
		if err := ValidateOfficialPaymentChannel(channels[i]); err != nil {
			return "", err
		}
	}

	data, err := common.Marshal(channels)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// 将内部渠道配置转为管理员可编辑的遮罩视图。
// 私钥、APIv3密钥和平台公钥字段都清空，已配置状态单独列名。
// 浏览器保存未修改字段时通过空值保留旧秘密，不必回传密钥。
// 数组中的渠道身份和交互信息保留，用于编辑已有渠道。
// JSON损坏时不得回传原文，因为原文可能仍包含完整私钥。
// 损坏配置会记录解析阶段错误，同时返回安全的空视图。
// 编码异常同样记录日志，不能泄露半成品JSON。
// ConfiguredSecrets只含字段名称，不包含长度、摘要或部分密钥。
// 没有设置值的秘密字段不标成已配置，避免前端误导管理员。
// 此函数只用于受保护的设置读取，普通用户接口另行投影字段。
func RedactOfficialPaymentChannels(raw string) string {
	var channels []OfficialPaymentChannel
	if err := common.UnmarshalJsonStr(raw, &channels); err != nil {
		common.SysError(common.LogText("failed to decode official payment channels for redaction: %v", err))
		return "[]"
	}
	for i := range channels {
		configured := make([]string, 0, 2) // 只保留字段名，绝不携带部分密钥正文。
		for _, key := range officialPaymentSecretKeys(channels[i].ChannelType) {
			if strings.TrimSpace(channels[i].Config[key]) != "" {
				configured = append(configured, key)
			}
			channels[i].Config[key] = ""
		}
		channels[i].ConfiguredSecrets = configured // 前端据此显示已设置提示。
	}
	data, err := common.Marshal(channels)
	if err != nil {
		common.SysError(common.LogText("failed to encode redacted official payment channels: %v", err))
		return "[]"
	}
	return string(data)
}

// 验证单个渠道的标识、交互、必填凭据和地址。
// 校验先复制配置映射，默认值填充不应修改全局已保存配置。
// ID只能包含路由安全字符，长度与数据库PaymentMethod字段一致。
// 显示名与图标有长度上限，防止配置无界增长。
// 最低充值量不能为负，不使用负值表达无限制。
// 支付宝仅允许扫码、页面或WAP三种已实现交互。
// 微信仅允许Native扫码和H5跳转，JSAPI未实现就不能配置启用。
// 停用渠道允许保存未填完凭据，但交互方式本身仍必须有效。
// 启用时必须提供该支付厂商所需的商户身份与密钥。
// APIv3密钥按字节长度验证，匹配微信AES256解密要求。
// 微信公钥ID具有固定前缀，不能用商户证书序列号代替。
// H5设备类型只能是已知枚举，不将任意文本发送给微信。
// 所有可配置回调及回跳地址统一验证为HTTP(S)地址。
// URL校验不发起请求，不依赖DNS或外网可用性。
// 未知支付厂商直接拒绝，不把错误类型静默当作支付宝。
// 错误保留明确字段和原因，避免仅提示配置失败难以定位。
// 密钥格式解析在实际签名服务中完成，此处不执行昂贵加密操作。
func ValidateOfficialPaymentChannel(channel OfficialPaymentChannel) error {
	// 校验不能修改共享配置映射，归一化只操作独立副本。
	channel.Config = clonePaymentConfig(channel.Config)
	normalizeOfficialPaymentChannel(&channel)
	if !officialPaymentChannelIDPattern.MatchString(channel.ID) {
		return common.NewMessage("Payment channel IDs may contain only letters, numbers, underscores and hyphens, up to 50 characters")
	}
	if channel.Name == "" || len(channel.Name) > 64 {
		return common.NewMessage("Payment channel names are required and must not exceed 64 bytes")
	}
	if len(channel.Icon) > 255 {
		return common.NewMessage("Payment channel icons must not exceed 255 bytes")
	}
	if channel.MinTopUp < 0 {
		return common.NewMessage("Minimum top-up cannot be negative")
	}

	switch channel.ChannelType {
	case PaymentChannelAlipay:
		if channel.InteractionMode != PaymentInteractionQR &&
			channel.InteractionMode != PaymentInteractionPage &&
			channel.InteractionMode != PaymentInteractionWAP {
			return common.NewMessage("Alipay interaction mode must be qr, page or wap")
		}
		if channel.Enabled {
			for _, key := range []string{"app_id", "private_key", "alipay_public_key"} {
				if strings.TrimSpace(channel.Config[key]) == "" {
					return common.NewMessage("Payment field {{field}} is required", map[string]any{"field": key})
				}
			}
		}
		if signType := strings.ToUpper(channel.Config["sign_type"]); signType != "RSA2" && signType != "RSA" {
			return common.NewMessage("Alipay signature type must be RSA2 or RSA")
		}
		if err := validateOfficialPaymentURL(channel.Config["gateway_url"], true); err != nil {
			return err
		}
		for _, key := range []string{"notify_url", "return_url"} {
			if err := validateOfficialPaymentURL(channel.Config[key], false); err != nil {
				return err
			}
		}
	case PaymentChannelWechat:
		if channel.InteractionMode != PaymentInteractionQR && channel.InteractionMode != PaymentInteractionRedirect {
			return common.NewMessage("WeChat Pay interaction mode must be qr or redirect")
		}
		if channel.Enabled {
			for _, key := range []string{"appid", "mchid", "merchant_serial_no", "merchant_private_key", "api_v3_key", "wechatpay_public_key_id", "wechatpay_public_key"} {
				if strings.TrimSpace(channel.Config[key]) == "" {
					return common.NewMessage("Payment field {{field}} is required", map[string]any{"field": key})
				}
			}
			if len(channel.Config["api_v3_key"]) != 32 {
				return common.NewMessage("WeChat Pay API v3 keys must be 32 bytes")
			}
			if !strings.HasPrefix(channel.Config["wechatpay_public_key_id"], "PUB_KEY_ID_") {
				return common.NewMessage("WeChat Pay public key IDs must start with PUB_KEY_ID_")
			}
		}
		if h5Type := strings.ToUpper(channel.Config["h5_type"]); h5Type != "WAP" && h5Type != "IOS" && h5Type != "ANDROID" {
			return common.NewMessage("WeChat Pay H5 type must be WAP, IOS or ANDROID")
		}
		for _, key := range []string{"notify_url", "h5_redirect_url", "h5_wap_url"} {
			if err := validateOfficialPaymentURL(channel.Config[key], false); err != nil {
				return err
			}
		}
	default:
		return common.NewMessage("Payment channel type must be alipay or wechat")
	}
	return nil
}

// 归一化渠道身份和枚举，保持订单查找的ID稳定。
// 只裁去配置值首尾空白，不改变密钥内部换行。
// 渠道类型和交互方式大小写统一后再做白名单验证。
// 空配置创建独立映射，后续写默认值不会触发空映射panic。
// 仅保留该支付厂商允许的配置字段，删除跨厂商遗留键。
// ConfiguredSecrets是展示信息，保存前必须清除。
// 支付宝默认官方网关与RSA2，不自行选择不安全的算法。
// 已明确提供的签名算法仍由后续校验决定是否允许。
// 微信H5设备类型默认WAP，其他配置按服务端枚举校验。
// 本函数会修改传入对象，调用者必须确保不共享原配置映射。
// 默认值只表达已有业务约定，不用于掩盖必填商户凭据缺失。
func normalizeOfficialPaymentChannel(channel *OfficialPaymentChannel) {
	channel.ID = strings.TrimSpace(channel.ID)
	channel.Name = strings.TrimSpace(channel.Name)
	channel.Icon = strings.TrimSpace(channel.Icon)
	channel.ChannelType = strings.ToLower(strings.TrimSpace(channel.ChannelType))
	channel.InteractionMode = strings.ToLower(strings.TrimSpace(channel.InteractionMode))
	channel.ConfiguredSecrets = nil // 保存值不携带读取视图的秘密状态。
	if channel.Config == nil {
		channel.Config = make(map[string]string)
	}

	allowed := officialPaymentConfigKeys(channel.ChannelType)
	for key, value := range channel.Config {
		if _, ok := allowed[key]; !ok {
			delete(channel.Config, key)
			continue
		}
		channel.Config[key] = strings.TrimSpace(value) // 保留PEM内部换行，不破坏签名材料。
	}
	if channel.ChannelType == PaymentChannelAlipay {
		if channel.Config["gateway_url"] == "" {
			channel.Config["gateway_url"] = "https://openapi.alipay.com/gateway.do"
		}
		if channel.Config["sign_type"] == "" {
			channel.Config["sign_type"] = "RSA2"
		} else {
			channel.Config["sign_type"] = strings.ToUpper(channel.Config["sign_type"])
		}
	} else if channel.ChannelType == PaymentChannelWechat {
		if channel.Config["h5_type"] == "" {
			channel.Config["h5_type"] = "WAP"
		} else {
			channel.Config["h5_type"] = strings.ToUpper(channel.Config["h5_type"])
		}
	}
}

// 定义每类渠道允许保存的协议字段。
// 通知地址是共用字段，厂商特有字段按类型分别列出。
// 支付宝签名配置不应残留在微信渠道，反之亦然。
// 白名单只用于配置输入，不作为普通用户可读字段清单。
// 返回新建集合，调用方删除字段不会改变其他请求的规则。
// 新增SDK能力时应同时增加这里及校验逻辑，避免前端配置被丢弃。
func officialPaymentConfigKeys(channelType string) map[string]struct{} {
	keys := []string{"notify_url"}
	if channelType == PaymentChannelAlipay {
		keys = append(keys, "app_id", "private_key", "alipay_public_key", "gateway_url", "return_url", "sign_type", "app_cert_sn", "alipay_root_cert_sn")
	} else if channelType == PaymentChannelWechat {
		keys = append(keys, "appid", "mchid", "merchant_serial_no", "merchant_private_key", "api_v3_key", "wechatpay_public_key_id", "wechatpay_public_key", "h5_redirect_url", "h5_type", "h5_wap_url", "h5_wap_name")
	}
	allowed := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		allowed[key] = struct{}{}
	}
	return allowed
}

// 定义设置读取必须遮罩的字段及保存时可保留的秘密。
// 同一份列表同时服务读取遮罩和空值合并，避免规则分叉。
// 平台验签公钥按原编辑流程也不回传原文。
// APIv3密钥用于通知解密，必须与商户私钥同等保护。
// 非秘密身份字段如应用ID仍可供管理员确认渠道。
// 未知厂商不产生秘密合并规则，并由渠道校验拒绝启用。
func officialPaymentSecretKeys(channelType string) []string {
	if channelType == PaymentChannelAlipay {
		return []string{"private_key", "alipay_public_key"}
	}
	if channelType == PaymentChannelWechat {
		return []string{"merchant_private_key", "api_v3_key", "wechatpay_public_key"}
	}
	return nil
}

// 验证支付配置中的地址格式，不访问地址指向的服务。
// 必填地址为空时直接报错，可选覆盖地址留空使用部署默认值。
// 只接受HTTP(S)与非空主机，不接受javascript或相对路径。
// URL解析失败不能保留部分结果继续保存。
// 此函数不对地址进行拼接，也不把主机解析为其他默认域名。
// 配置仅允许超级管理员修改，不接受付款用户临时覆盖。
func validateOfficialPaymentURL(raw string, required bool) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if required {
			return common.NewMessage("Payment URL is required")
		}
		return nil
	}
	// 仅检查协议和主机格式，不发起网络探测。
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return common.NewMessage("Payment URL must be a valid HTTP(S) URL")
	}
	return nil
}

// 复制渠道配置映射，确保读取和校验互不污染原配置。
// 字符串值本身不可变，浅复制已足够，不引入序列化往返。
// 空输入产生可写的空映射，供归一化器补默认值。
// 复制结果仍包含私密字段，只能供后端服务使用。
// 此工具不执行遮罩；公开读取必须另走专用函数。
func clonePaymentConfig(config map[string]string) map[string]string {
	copy := make(map[string]string, len(config))
	for key, value := range config {
		copy[key] = value
	}
	return copy
}
