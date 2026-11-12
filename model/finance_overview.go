package model

import (
	"cmp"
	"fmt"
	"math"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// 财务总览是只读聚合：只从已有订单、挂帐账本和发票申请读取，不写任何资金状态。
// 口径约定（与产品确认）：
//   - 实收按到账时间一次性计入，客户订阅不按周期摊销；
//   - 挂帐发生时只算待收，财务登记结清的时间点才转入实收；
//   - 余额支付的代充值不计收入，那笔钱在钱包充值时已经算过一次；
//   - 只合计人民币渠道，其他网关按原始金额单列，不折算也不相加。

// 上海固定为 UTC+8 且没有夏令时，用固定偏移分日，不依赖运行环境的时区数据库。
const financeZoneOffset int64 = 8 * 3600

// FinanceOverviewMaxDays 限制单次查询跨度，避免逐日序列和扫描范围无界增长。
const FinanceOverviewMaxDays = 366

// 挂帐超过该天数仍未结清视为超期；授信目前没有约定账期，统一按 30 天提示。
const financeOverdueDays = 30

// 这些渠道的订单金额确定为人民币；其余网关的币种由各自配置决定，不能直接相加。
var financeCNYProviders = []string{PaymentProviderEpay, PaymentProviderWechat, PaymentProviderAlipay}

// FinanceChannelCreditSettlement 标识线下结清的挂帐款，与在线支付渠道并列展示。
const FinanceChannelCreditSettlement = "credit_settlement"

// FinanceDaily 是一个上海自然日内各业务线的实收，Day 为该日零点的 Unix 秒。
type FinanceDaily struct {
	Day                int64 `json:"day"`                  // 上海该自然日零点的 Unix 秒，作为逐日序列的键。
	TopupCents         int64 `json:"topup_cents"`          // 当日钱包充值实收，人民币分。
	SubscriptionCents  int64 `json:"subscription_cents"`   // 当日订阅实收，按付款日一次性计入。
	RechargeCents      int64 `json:"recharge_cents"`       // 当日代充值实收，不含余额支付和挂帐。
	CreditSettledCents int64 `json:"credit_settled_cents"` // 当日登记结清的挂帐款。
}

// FinanceChannel 按收款渠道汇总实收；Channel 为支付渠道标识或挂帐结清。
type FinanceChannel struct {
	Channel string `json:"channel"` // 支付渠道标识，挂帐结清使用固定标识。
	Cents   int64  `json:"cents"`   // 该渠道时段内实收，人民币分。
	Count   int64  `json:"count"`   // 该渠道时段内成功订单或结清笔数。
}

// FinanceOtherGateway 单列未纳入人民币实收的订单记录值，不据此推断现金到账。
type FinanceOtherGateway struct {
	Provider string  `json:"provider"` // 网关标识，原样返回不翻译。
	Money    float64 `json:"money"`    // 订单存储值；部分网关保存的是额度面额，不是真实收款。
	Count    int64   `json:"count"`    // 该网关时段内成功订单数。
}

// FinanceDebtor 是未结清欠款最多的用户之一，名称为当前值而非下单时快照。
type FinanceDebtor struct {
	UserID      int    `json:"user_id"`      // 欠款人编号，用户删除后仍保留。
	Username    string `json:"username"`     // 当前用户名，用户已删除时为空串。
	DisplayName string `json:"display_name"` // 当前显示名，可能为空。
	Cents       int64  `json:"cents"`        // 该用户未结清欠款合计，人民币分。
	Count       int64  `json:"count"`        // 该用户未结清的挂帐笔数。
}

// FinanceCredit 汇总挂帐：除 NewCents 属于查询时段外，其余都是当前时点快照。
type FinanceCredit struct {
	OutstandingCents int64           `json:"outstanding_cents"`    // 全部未结清欠款，当前时点。
	OutstandingCount int64           `json:"outstanding_count"`    // 未结清挂帐笔数。
	LimitCents       int64           `json:"limit_cents" gorm:"-"` // 仅统计已启用授信的上限。
	UsedCents        int64           `json:"used_cents" gorm:"-"`  // 含已停用账户的遗留占用。
	NewCents         int64           `json:"new_cents" gorm:"-"`   // 时段内新发生的挂帐。
	Aging30Cents     int64           `json:"aging30_cents"`        // 账龄 30 天以内。
	Aging60Cents     int64           `json:"aging60_cents"`        // 账龄 31 至 60 天。
	AgingOverCents   int64           `json:"aging_over_cents"`     // 账龄超过 60 天。
	OverdueCount     int64           `json:"overdue_count"`        // 超过 30 天未结清的笔数。
	TopDebtors       []FinanceDebtor `json:"top_debtors" gorm:"-"` // 欠款最多的前五名用户。
}

// FinanceTodo 是需要人工跟进的事项数量，对应财务与代充值的现有办理页面。
type FinanceTodo struct {
	PendingInvoices int64 `json:"pending_invoices"` // 已付款但尚未开具或拒绝的开票申请数。
	PendingRefunds  int64 `json:"pending_refunds"`  // 已申请或已批准但尚未完成的代充值退款数。
}

// FinanceOverview 是总览接口的完整响应；金额一律为人民币整数分，额度为平台内部单位。
type FinanceOverview struct {
	Start              int64 `json:"start"`                // 查询时段起点，含。
	End                int64 `json:"end"`                  // 查询时段终点，不含。
	TopupCents         int64 `json:"topup_cents"`          // 时段内钱包充值实收。
	SubscriptionCents  int64 `json:"subscription_cents"`   // 时段内订阅实收。
	RechargeCents      int64 `json:"recharge_cents"`       // 时段内代充值实收。
	CreditSettledCents int64 `json:"credit_settled_cents"` // 时段内结清的挂帐款。
	ReceivedCents      int64 `json:"received_cents"`       // 四条业务线实收合计。
	RefundCents        int64 `json:"refund_cents"`         // 时段内登记的现金退款。
	NetCents           int64 `json:"net_cents"`            // 实收减退款。
	InvoiceFeeCents    int64 `json:"invoice_fee_cents"`    // 实收中包含的开票附加费。

	Daily         []FinanceDaily        `json:"daily"`          // 逐日序列，覆盖时段内每个上海自然日。
	Channels      []FinanceChannel      `json:"channels"`       // 按收款渠道汇总，金额降序。
	OtherGateways []FinanceOtherGateway `json:"other_gateways"` // 非人民币网关，单列不合计。
	Credit        FinanceCredit         `json:"credit"`         // 挂帐快照与时段内新增。

	WalletBalanceQuota int64 `json:"wallet_balance_quota"` // 钱包额度合计，含赠送和手动调额，不等于现金。
	ConsumedQuota      int64 `json:"consumed_quota"`       // 时段内消耗的额度，含订阅与钱包。
	// 消耗来自按小时汇总的看板数据；该功能关闭时没有数据，不能把零当成没有消耗。
	ConsumptionAvailable bool `json:"consumption_available"`

	Todo              FinanceTodo         `json:"todo"`               // 需要人工跟进的事项数量。
	OfflineCashCents  int64               `json:"offline_cash_cents"` // 已含在对应业务线实收中，不再次相加。
	WriteOffCents     int64               `json:"write_off_cents"`    // 按本期核销时间统计的坏账，不是现金支出。
	WriteOffCount     int64               `json:"write_off_count"`    // 本期核销笔数。
	ManualAdjustments []QuotaLedgerTotals `json:"manual_adjustments"` // 人工发放/更正分来源分原因统计。
}

// financeBucket 是一次分组查询的一行：某自然日、某渠道的金额与笔数。
type financeBucket struct {
	BucketDay   int64   // 所属上海自然日零点。
	BucketLabel string  // 分组标签：支付渠道或挂帐模块。
	Amount      float64 // 金额之和；单位取决于来源表，元或分。
	Cnt         int64   // 该分组的记录数。
}

// financeBuckets 把调用方已限定时间范围的查询按上海自然日和渠道分组求和。
// 列名和金额表达式只接受本文件内的常量，不拼接任何外部输入。
// 取模在三种数据库的整数列上语义一致，避免使用各方言不同的日期函数。
func financeBuckets(query *gorm.DB, timeCol, labelCol, amountCol string) ([]financeBucket, error) {
	day := fmt.Sprintf("%s - ((%s + %d) %% 86400)", timeCol, timeCol, financeZoneOffset)
	var rows []financeBucket
	err := query.Select(fmt.Sprintf("%s AS bucket_day, %s AS bucket_label, COALESCE(SUM(%s), 0) AS amount, COUNT(*) AS cnt", day, labelCol, amountCol)).
		Group(day + ", " + labelCol).Scan(&rows).Error
	return rows, err
}

// GetFinanceOverview 汇总 [start, end) 时段的收入与当前挂帐、钱包、待办快照。
// 调用方负责校验时间范围；任何一步查询失败都整体返回错误，不输出部分数字。
func GetFinanceOverview(start, end int64) (*FinanceOverview, error) {
	overview := &FinanceOverview{
		Start: start, End: end,
		Channels: []FinanceChannel{}, OtherGateways: []FinanceOtherGateway{},
		ConsumptionAvailable: common.DataExportEnabled && common.LogConsumeEnabled,
	}
	// 逐日序列先铺满零值，没有收入的日期也要出现在趋势图上。
	firstDay := start - ((start + financeZoneOffset) % 86400)
	for day := firstDay; day < end; day += 86400 {
		overview.Daily = append(overview.Daily, FinanceDaily{Day: day})
	}

	// 订阅到账会同时写一条 TopUp 账单镜像，按交易号关联排除，不能重复统计。
	// 不按 provider 为空直接排除，避免把确实存在的旧钱包订单一起丢弃。
	subscriptionMirror := DB.Model(&SubscriptionOrder{}).Select("1").Where("subscription_orders.trade_no = top_ups.trade_no")
	topups, err := financeBuckets(DB.Model(&TopUp{}).Where("NOT EXISTS (?)", subscriptionMirror).
		Where("payment_provider <> ?", PaymentProviderCredit).
		Where("status = ? AND complete_time >= ? AND complete_time < ?", common.TopUpStatusSuccess, start, end),
		"complete_time", "payment_provider", "money")
	if err != nil {
		return nil, err
	}
	subscriptions, err := financeBuckets(DB.Model(&SubscriptionOrder{}).Where("payment_provider <> ?", PaymentProviderCredit).
		Where("status = ? AND complete_time >= ? AND complete_time < ?", common.TopUpStatusSuccess, start, end),
		"complete_time", "payment_provider", "money")
	if err != nil {
		return nil, err
	}
	// 代充值排除余额支付（重复计收入）和挂帐（尚未收款，且 paid_at 保持为零）。
	// 之后退款的订单当时确实收到了钱，仍计入收款日，退款在退款日单独冲减。
	recharges, err := financeBuckets(DB.Model(&AccountRechargeOrder{}).Where("paid_at >= ? AND paid_at < ? AND payment_provider NOT IN ?", start, end, []string{"wallet", "credit"}),
		"paid_at", "payment_provider", "money_cents")
	if err != nil {
		return nil, err
	}
	// 挂帐以财务登记结清的时间计入；结清后又退款的记录保留原结清时间。
	settlements, err := financeBuckets(DB.Model(&CreditEntry{}).Where("settled_at >= ? AND settled_at < ?", start, end),
		"settled_at", "module", "amount_cents")
	if err != nil {
		return nil, err
	}

	channels := map[string]*FinanceChannel{}
	others := map[string]*FinanceOtherGateway{}
	for _, source := range []struct {
		name string
		rows []financeBucket
		yuan bool // 金额列以元为单位时需要换算为分。
	}{
		{"topup", topups, true},
		{"subscription", subscriptions, true},
		{"recharge", recharges, false},
		{"credit", settlements, false},
	} {
		for _, row := range source.rows {
			channel := row.BucketLabel
			if source.name == "credit" {
				channel = FinanceChannelCreditSettlement
			} else if !slices.Contains(financeCNYProviders, row.BucketLabel) {
				// 这些字段未统一冻结实收币种和金额，只列存储值供追溯，不参与收入合计。
				other := others[row.BucketLabel]
				if other == nil {
					other = &FinanceOtherGateway{Provider: row.BucketLabel}
					others[row.BucketLabel] = other
				}
				other.Money += row.Amount
				other.Count += row.Cnt
				continue
			}
			cents := int64(row.Amount)
			if source.yuan {
				cents = int64(math.Round(row.Amount * 100))
			}
			daily := &overview.Daily[(row.BucketDay-firstDay)/86400]
			switch source.name {
			case "topup":
				daily.TopupCents += cents
				overview.TopupCents += cents
			case "subscription":
				daily.SubscriptionCents += cents
				overview.SubscriptionCents += cents
			case "recharge":
				daily.RechargeCents += cents
				overview.RechargeCents += cents
			case "credit":
				daily.CreditSettledCents += cents
				overview.CreditSettledCents += cents
			}
			total := channels[channel]
			if total == nil {
				total = &FinanceChannel{Channel: channel}
				channels[channel] = total
			}
			total.Cents += cents
			total.Count += row.Cnt
		}
	}
	// 线下收款按人工记录发生日计入所属业务线，渠道单独展示，不造网关账单。
	// 调额后消费不再计一次收入，后台开订阅也不重复生成 TopUp 镜像。
	manualCash, err := financeBuckets(DB.Model(&QuotaLedger{}).
		Where("reason = ? AND created_at >= ? AND created_at < ?", QuotaAdjustReasonOnsiteCash, start, end), "created_at", "source", "cash_cents")
	if err != nil {
		return nil, err
	}
	for _, row := range manualCash {
		cents := int64(row.Amount)
		daily := &overview.Daily[(row.BucketDay-firstDay)/86400]
		switch row.BucketLabel {
		case ManualSourceQuota:
			overview.TopupCents += cents
			daily.TopupCents += cents
		case ManualSourceSubscription:
			overview.SubscriptionCents += cents
			daily.SubscriptionCents += cents
		default:
			return nil, ErrQuotaLedgerInvalid // 未知来源不能静默混入现金报表。
		}
		overview.OfflineCashCents += cents
		channel := channels[QuotaAdjustReasonOnsiteCash]
		if channel == nil {
			channel = &FinanceChannel{Channel: QuotaAdjustReasonOnsiteCash}
			channels[QuotaAdjustReasonOnsiteCash] = channel
		}
		channel.Cents += cents
		channel.Count += row.Cnt
	}
	overview.ManualAdjustments, err = SumQuotaLedgers(start, end)
	if err != nil {
		return nil, err
	}
	// 核销只记录损失和归还占用，不能把原应收金额再次作为现金支出扣除。
	if err := DB.Model(&CreditEntry{}).Where("status = ? AND closed_at >= ? AND closed_at < ?", "write_off", start, end).
		Select("COALESCE(SUM(amount_cents), 0)").Scan(&overview.WriteOffCents).Error; err != nil {
		return nil, err
	}
	if err := DB.Model(&CreditEntry{}).Where("status = ? AND closed_at >= ? AND closed_at < ?", "write_off", start, end).
		Count(&overview.WriteOffCount).Error; err != nil {
		return nil, err
	}
	for _, channel := range channels {
		overview.Channels = append(overview.Channels, *channel)
	}
	// 映射遍历无序，按金额降序并以名称兜底，保证相同数据得到相同响应。
	slices.SortFunc(overview.Channels, func(a, b FinanceChannel) int {
		return cmp.Or(cmp.Compare(b.Cents, a.Cents), cmp.Compare(a.Channel, b.Channel))
	})
	for _, other := range others {
		overview.OtherGateways = append(overview.OtherGateways, *other)
	}
	slices.SortFunc(overview.OtherGateways, func(a, b FinanceOtherGateway) int {
		return cmp.Compare(a.Provider, b.Provider)
	})
	overview.ReceivedCents = overview.TopupCents + overview.SubscriptionCents + overview.RechargeCents + overview.CreditSettledCents

	// 现金退款有两类：在线付款的代充值订单按退款事件时间计，已结清挂帐按关闭时间计。
	// 充值和订阅订单没有退款状态，在商户后台直接退的款项平台无从得知，不在此列。
	var rechargeRefund, creditRefund int64
	if err := DB.Table("account_recharge_events").
		Joins("JOIN account_recharge_orders ON account_recharge_orders.id = account_recharge_events.order_id").
		Where("account_recharge_events.action = ? AND account_recharge_events.created_at >= ? AND account_recharge_events.created_at < ?", "refund", start, end).
		Where("account_recharge_orders.payment_provider NOT IN ?", []string{"wallet", "credit"}).
		Select("COALESCE(SUM(account_recharge_orders.money_cents), 0)").Scan(&rechargeRefund).Error; err != nil {
		return nil, err
	}
	if err := DB.Model(&CreditEntry{}).Where("status = ? AND closed_at >= ? AND closed_at < ?", "refunded", start, end).
		Select("COALESCE(SUM(amount_cents), 0)").Scan(&creditRefund).Error; err != nil {
		return nil, err
	}
	overview.RefundCents = rechargeRefund + creditRefund
	overview.NetCents = overview.ReceivedCents - overview.RefundCents

	// 开票附加费已经包含在各订单实付里，这里只说明其中有多少是附加费。
	var invoiceFee float64
	if err := DB.Model(&InvoiceRequest{}).Where("paid_at >= ? AND paid_at < ?", start, end).
		Select("COALESCE(SUM(fee_amount), 0)").Scan(&invoiceFee).Error; err != nil {
		return nil, err
	}
	overview.InvoiceFeeCents = int64(math.Round(invoiceFee * 100))

	// 未结清欠款与账龄取当前时点，和查询时段无关：欠款不会因为切换时段而消失。
	now := common.GetTimestamp()
	overdue, old := now-financeOverdueDays*86400, now-2*financeOverdueDays*86400
	if err := DB.Model(&CreditEntry{}).Where("status = ?", "outstanding").
		Select("COALESCE(SUM(amount_cents), 0) AS outstanding_cents, COUNT(*) AS outstanding_count, "+
			"COALESCE(SUM(CASE WHEN created_at >= ? THEN amount_cents ELSE 0 END), 0) AS aging30_cents, "+
			"COALESCE(SUM(CASE WHEN created_at < ? AND created_at >= ? THEN amount_cents ELSE 0 END), 0) AS aging60_cents, "+
			"COALESCE(SUM(CASE WHEN created_at < ? THEN amount_cents ELSE 0 END), 0) AS aging_over_cents, "+
			"COALESCE(SUM(CASE WHEN created_at < ? THEN 1 ELSE 0 END), 0) AS overdue_count",
			overdue, overdue, old, old, overdue).Scan(&overview.Credit).Error; err != nil {
		return nil, err
	}
	if err := DB.Model(&CreditAccount{}).Where("enabled = ?", true).
		Select("COALESCE(SUM(limit_cents), 0)").Scan(&overview.Credit.LimitCents).Error; err != nil {
		return nil, err
	}
	if err := DB.Model(&CreditAccount{}).Select("COALESCE(SUM(used_cents), 0)").Scan(&overview.Credit.UsedCents).Error; err != nil {
		return nil, err
	}
	if err := DB.Model(&CreditEntry{}).Where("created_at >= ? AND created_at < ?", start, end).
		Select("COALESCE(SUM(amount_cents), 0)").Scan(&overview.Credit.NewCents).Error; err != nil {
		return nil, err
	}
	// 用户被删除后欠款仍在，左连接保留账本行，名称缺失时返回空串。
	overview.Credit.TopDebtors = []FinanceDebtor{}
	if err := DB.Table("credit_entries").Joins("LEFT JOIN users ON users.id = credit_entries.user_id").
		Where("credit_entries.status = ?", "outstanding").
		Select("credit_entries.user_id AS user_id, COALESCE(MAX(users.username), '') AS username, COALESCE(MAX(users.display_name), '') AS display_name, SUM(credit_entries.amount_cents) AS cents, COUNT(*) AS count").
		Group("credit_entries.user_id").Order("cents DESC, credit_entries.user_id").Limit(5).
		Scan(&overview.Credit.TopDebtors).Error; err != nil {
		return nil, err
	}

	if err := DB.Model(&User{}).Select("COALESCE(SUM(quota), 0)").Scan(&overview.WalletBalanceQuota).Error; err != nil {
		return nil, err
	}
	if overview.ConsumptionAvailable {
		if err := DB.Model(&QuotaData{}).Where("created_at >= ? AND created_at < ?", start, end).
			Select("COALESCE(SUM(quota), 0)").Scan(&overview.ConsumedQuota).Error; err != nil {
			return nil, err
		}
	}

	// 待办只给数量，办理仍在各自页面完成；退款含已申请未批准和已批准未确认两种。
	if err := DB.Model(&InvoiceRequest{}).Where("status IN ?", []string{"pending", "processing"}).
		Count(&overview.Todo.PendingInvoices).Error; err != nil {
		return nil, err
	}
	if err := DB.Model(&AccountRechargeOrder{}).
		Where("payment_status = ? OR (payment_status = ? AND refund_requested = ?)", "refunding", "paid", true).
		Count(&overview.Todo.PendingRefunds).Error; err != nil {
		return nil, err
	}
	return overview, nil
}
