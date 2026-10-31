package controller

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 人民币输入是面额，不是美元数量；应付和到账必须由真实报价入口一起验证。
// 不调用实际商户系统，HTTP Recorder 仅替代外部网络请求边界。
// 测试独立还原货币、分组和套餐优惠配置，不影响其他支付场景。
func TestCNYTopUpQuoteUsesFaceValueAndReturnsCreditedQuota(t *testing.T) {
	gin.SetMode(gin.TestMode)
	general := operation_setting.GetGeneralSetting()
	oldDisplay, oldPrice, oldRate := general.QuotaDisplayType, operation_setting.Price, operation_setting.USDExchangeRate // 展示、售价与换算各自保存，测试不永久改价。
	oldQuota, oldMin := common.QuotaPerUnit, operation_setting.MinTopUp                                                   // 额度精度和最低面额与报价结果相关。
	oldDiscount := operation_setting.GetPaymentSetting().AmountDiscount                                                   // 保留原优惠表，避免影响后续付款测试。
	oldGroups := common.TopupGroupRatio2JSONString()                                                                      // 分组优惠也来自生产配置，而不是在测试复制计算。
	// 使用既有真实数据库初始化，列名方言与生产入口保持一致。
	db := modelManagementDB(t, "sqlite", "") // 复用现有隔离库与方言初始化，不连接开发数据库。
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "cny-quote", Group: "default"}).Error)
	general.QuotaDisplayType = operation_setting.QuotaDisplayTypeCNY // 只对用户确认的人民币支付规则作新断言。
	common.QuotaPerUnit, operation_setting.MinTopUp = 500000, 1      // 明确币种换算精度，不依赖其他测试残留。
	operation_setting.Price = 7.3                                    // 保留触发问题的旧美元售价，人民币输入不再乘它。
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1}`))
	t.Cleanup(func() {
		general.QuotaDisplayType, operation_setting.Price, operation_setting.USDExchangeRate = oldDisplay, oldPrice, oldRate
		common.QuotaPerUnit, operation_setting.MinTopUp = oldQuota, oldMin
		operation_setting.GetPaymentSetting().AmountDiscount = oldDiscount
		assert.NoError(t, common.UpdateTopupGroupRatioByJSONString(oldGroups))
	})
	for _, scenario := range []struct {
		name     string
		rate     float64
		discount float64
		pay      string
		quota    int
	}{
		{"default exchange", 7.3, 1, "1.00", 68493}, // 1元按内部美元单位存储，不能入账1美元。
		{"updated exchange", 10, 1, "1.00", 50000},  // 修改换算只改变内部额度，不改变付款面额。
		{"preset discount", 10, 0.8, "0.80", 50000}, // 优惠减少应付，但不减少所选择的充值面额。
	} {
		t.Run(scenario.name, func(t *testing.T) {
			operation_setting.USDExchangeRate = scenario.rate                                            // 改变存储换算时，应付面额仍然不变。
			operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{1: scenario.discount} // 优惠按原输入面额命中。
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Set("id", 1) // 公共入口从会话上下文获得用户，HTTP请求不能指定到账账户。
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/amount", strings.NewReader(`{"amount":1}`))
			ctx.Request.Header.Set("Content-Type", "application/json") // 保留生产JSON绑定，不能绕过金额类型解析。
			RequestAmount(ctx)
			var response struct {
				Message       string `json:"message"`        // 沿用原报价响应，不通过新接口绕过付款校验。
				Data          string `json:"data"`           // 应付数值仍保留两位小数的字符串合同。
				CreditedQuota int    `json:"credited_quota"` // 新字段必须给出可用于后续下单的准确整数额度。
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.Equal(t, "success", response.Message)
			assert.Equal(t, scenario.pay, response.Data)            // 直接断言用户收到的应付，不复制乘法公式。
			assert.Equal(t, scenario.quota, response.CreditedQuota) // 对外额度与后续订单使用同一结果。
		})
	}
	// 真实易支付下单只生成签名表单，不向虚构商户域名发起交易。
	// 验证网关 money 字段和订单冻结额度，避免报价修好但实际仍收七点三。
	t.Run("checkout persists the same quote", func(t *testing.T) {
		payment := operation_setting.GetPaymentSetting()                                                               // 只在虚构易支付订单测试中开启已有测试配置。
		previous := *payment                                                                                           // 保存完整结构，复原时不遗漏原有的其他支付配置。
		oldAddress, oldID, oldKey := operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey // 实际商户材料不进入请求fixture。
		t.Cleanup(func() {
			*payment = previous // 只修改内存测试配置，不持久化或更改真实商户材料。
			operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = oldAddress, oldID, oldKey
		})
		payment.ComplianceConfirmed, payment.ComplianceTermsVersion = true, operation_setting.CurrentComplianceTermsVersion
		payment.AmountDiscount = map[int]float64{} // 无优惠时一元面额就应收一元。
		operation_setting.PayAddress, operation_setting.EpayId, operation_setting.EpayKey = "https://payment.example", "fixture-merchant", "fixture-key"
		operation_setting.USDExchangeRate = 7.3 // 与原问题一致的数值用于验证下单冻结，不代表硬编码生产汇率。
		require.NoError(t, db.AutoMigrate(&model.TopUp{}))
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Set("id", 1) // 公共入口从会话上下文获得用户，HTTP请求不能指定到账账户。
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/pay", strings.NewReader(`{"amount":1,"payment_method":"wxpay"}`))
		ctx.Request.Header.Set("Content-Type", "application/json") // 保留生产JSON绑定，不能绕过金额类型解析。
		RequestEpay(ctx)
		var response struct {
			Message string            `json:"message"`
			Data    map[string]string `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.Equal(t, "success", response.Message)
		assert.Equal(t, "1.00", response.Data["money"]) // 核对交给网关的实际支付字段。
		var order model.TopUp
		require.NoError(t, db.Where("trade_no = ?", response.Data["out_trade_no"]).First(&order).Error)
		assert.EqualValues(t, 68493, order.CreditedQuota) // 后续回调只可发放这份冻结权益。
		assert.Equal(t, 1.0, order.Money)                 // 数据库保存的实收与发给网关的报价一致。
	})

}

// 货币转换是资金入口，零、负数和非法浮点汇率必须明确报错。
// 不通过默认七点三或返回零报价掩盖坏配置，更不能继续创建支付单。
func TestCNYTopUpRejectsInvalidExchangeRate(t *testing.T) {
	general := operation_setting.GetGeneralSetting()
	oldDisplay, oldRate := general.QuotaDisplayType, operation_setting.USDExchangeRate
	general.QuotaDisplayType = operation_setting.QuotaDisplayTypeCNY // 只对用户确认的人民币支付规则作新断言。
	t.Cleanup(func() { general.QuotaDisplayType, operation_setting.USDExchangeRate = oldDisplay, oldRate })
	for _, rate := range []float64{0, -1, math.NaN(), math.Inf(1)} { // 不把无效换算降级为默认售价。
		operation_setting.USDExchangeRate = rate
		_, err := getEpayTopUpQuota(1) // 校验发生在任何订单创建和钱包写入之前。
		require.EqualError(t, err, "Currency exchange rate must be positive and finite")
	}
}

// 只修人民币本地收款，美元模式保留原有售价而不强行按一比一收款。
// 自定义币种与 Token 模式也不能随人民币修复被隐式改价。
func TestLocalTopUpKeepsOtherInputContracts(t *testing.T) {
	general := operation_setting.GetGeneralSetting()
	oldDisplay, oldPrice := general.QuotaDisplayType, operation_setting.Price
	oldDiscount := operation_setting.GetPaymentSetting().AmountDiscount // 保留原优惠表，避免影响后续付款测试。
	oldGroups := common.TopupGroupRatio2JSONString()                    // 分组优惠也来自生产配置，而不是在测试复制计算。
	t.Cleanup(func() {
		general.QuotaDisplayType, operation_setting.Price = oldDisplay, oldPrice
		operation_setting.GetPaymentSetting().AmountDiscount = oldDiscount
		assert.NoError(t, common.UpdateTopupGroupRatioByJSONString(oldGroups))
	})
	operation_setting.Price = 7.3 // 非人民币渠道仍由管理员原售价控制。
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1}`))
	for _, display := range []string{operation_setting.QuotaDisplayTypeUSD, operation_setting.QuotaDisplayTypeCustom} {
		general.QuotaDisplayType = display
		assert.InDelta(t, 7.3, getPayMoney(1, "default"), 0.000001) // 反例保护旧USD数量语义不受修复影响。
		quota, err := getEpayTopUpQuota(1)
		require.NoError(t, err)
		assert.EqualValues(t, common.QuotaPerUnit, quota) // 一美元数量仍换成原内部单位，不误用CNY汇率。
	}
}
