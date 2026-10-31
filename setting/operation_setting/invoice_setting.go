package operation_setting

import (
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/shopspring/decimal"
)

// InvoiceSetting 只保存加价规则，用户抬头和发票附件不放入公开站点配置。
// 百分数直接用6表示6%，各分组可显式配置0来覆盖默认费率。
// 费用在下单时冻结，后续修改不会改变已有订单的实付或开票金额。
type InvoiceSetting struct {
	DefaultRate float64            `json:"default_rate"` // 默认加价只影响用户勾选发票的订单。
	GroupRates  map[string]float64 `json:"group_rates"`  // 键是实际用户分组，值是覆盖百分数。
}

var invoiceSetting = InvoiceSetting{DefaultRate: 0, GroupRates: map[string]float64{}} // 未设置附加费时选择发票不额外收费。

func init() {
	config.GlobalConfig.Register("invoice_setting", &invoiceSetting) // 复用现有系统选项持久化，无新配置表。
}

// InvoiceRate 由用户真实分组选择，不接受浏览器提交费率。
// 无组覆盖时采用默认；非法配置必须报错，不能静默免收开票附加费。
func InvoiceRate(group string) (float64, error) {
	rate := invoiceSetting.DefaultRate // 不存在组覆盖才使用默认费率。
	if override, exists := invoiceSetting.GroupRates[group]; exists {
		rate = override // 显式零覆盖也有效，不能按真假值忽略。
	}
	if err := validateInvoiceRate(rate); err != nil {
		return 0, err
	}
	return rate, nil
}

// ValidateInvoiceSettingUpdate 在通用设置写入之前验证新值。
// 不校验不存在的未来分组，但限制键长度和空键，避免坏配置不可管理。
func ValidateInvoiceSettingUpdate(key, value string) error {
	if key == "invoice_setting.default_rate" { // 百分比保存与组映射分别校验。
		var rate *float64 // null不能被当作明确配置的零费用。
		if err := common.UnmarshalJsonStr(value, &rate); err != nil || rate == nil {
			return common.NewMessage("Invoice surcharge percentage must be between 0 and 100")
		}
		return validateInvoiceRate(*rate)
	}
	var rates map[string]*float64 // 指针区分缺失数值和有效零覆盖。
	if err := common.UnmarshalJsonStr(value, &rates); err != nil || rates == nil {
		return common.NewMessage("Invoice group surcharge rates must be a JSON object")
	}
	for group, rate := range rates { // 任意一项坏配置都拒绝整次写入，避免静默丢组。
		if strings.TrimSpace(group) == "" || len(group) > 64 {
			return common.NewMessage("Invalid invoice surcharge group")
		}
		if rate == nil {
			return common.NewMessage("Invoice surcharge percentage must be between 0 and 100")
		}
		if err := validateInvoiceRate(*rate); err != nil {
			return err
		}
	}
	return nil // 只做校验，真正持久化仍由受RootAuth保护的选项接口完成。
}

// 百分比的边界同时覆盖保存和运行时读取，不以浮点比较掩盖NaN。
// 零费率是有效配置，财务仍会收到需要人工处理的申请。
func validateInvoiceRate(rate float64) error {
	if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 || rate > 100 {
		return common.NewMessage("Invoice surcharge percentage must be between 0 and 100")
	}
	// 费率快照按四位小数存储，不能先按更高精度收费再截断记录。
	if !decimal.NewFromFloat(rate).Equal(decimal.NewFromFloat(rate).Round(4)) {
		return common.NewMessage("Invoice surcharge percentage supports at most 4 decimal places")
	}
	return nil
}
