package model

import (
	"context"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// 创建时冻结金额、汇率和支付方式；
// 客户端幂等键仅在本人范围唯一。
// 相同键参数不同属于冲突，
// 不能悄悄创建第二笔扣款。
// 商品版本在事务内再检查，
// 报价之后改价要求用户重新确认。
// 请求恢复先查持久结果，
// 不能因网络超时生成第二个订单号。
// 唯一约束是跨进程幂等依据，
// 进程锁不能代替数据库约束。
// 发票快照与业务订单同事务创建，
// 不能出现孤立收费申请。
// 商品价格与版本在行锁中核对，
// 不能仅相信控制器的预读。
// 订单标题和说明以本次购买为准，
// 后续商品编辑不追溯修改。
// 创建成功不等于付款成功，
// 待提交资料也必须先满足已付状态。
// 余额扣款另有独立入口，
// 纯创建失败不会消费钱包。
// 碰到唯一键竞争时只恢复摘要相同的请求，
// 不把所有错误当重复。
// 订单号由服务端生成，
// 不能接收浏览器指定的其他业务交易号。
func CreateAccountRechargeOrder(o *AccountRechargeOrder, productVersion int, invoice *InvoiceRequest) (*AccountRechargeOrder, error) {
	var existing AccountRechargeOrder
	if err := DB.Where("user_id = ? AND request_id = ?", o.UserID, o.RequestID).First(&existing).Error; err == nil {
		if existing.RequestHash != o.RequestHash {
			return nil, ErrRechargeConflict
		}
		return &existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var p AccountRechargeProduct
		if err := lockForUpdate(tx).First(&p, o.ProductID).Error; err != nil {
			return err
		}
		if !p.Enabled || p.Version != productVersion || p.PriceCents != o.BaseCents || (o.PaymentProvider == "wallet" && !p.AllowWalletPayment) {
			return ErrRechargeUnavailable
		}
		o.Title, o.Description = p.Title, p.Description
		// 在商品锁定的事务内冻结展示信息，后续编辑不改变已购套餐。
		// 不从报价请求接收图标或说明，防止客户端伪造历史内容。
		o.Summary, o.Provider, o.Period = p.Summary, p.Provider, p.Period
		o.DeliveryTime, o.Icon = p.DeliveryTime, p.Icon
		o.PaymentStatus, o.Status, o.Version = "pending", "awaiting_info", 1
		o.CreatedAt, o.UpdatedAt = common.GetTimestamp(), common.GetTimestamp()
		if err := tx.Create(o).Error; err != nil {
			return err
		}
		return CreateInvoiceForOrderTx(tx, invoice, o.TradeNo)
	})
	if err != nil {
		// 唯一键竞争只在能读取同一请求快照时视为幂等成功。
		if readErr := DB.Where("user_id = ? AND request_id = ?", o.UserID, o.RequestID).First(&existing).Error; readErr == nil && existing.RequestHash == o.RequestHash {
			return &existing, nil
		}
	}
	return o, err
}

// 官方微信和网关微信验签后共用金额核对，
// 不能给平台钱包加额。
// 迟到通知仍按真实收款接收；
// 重复通知不重复推进版本或开票。
// 提供商、渠道、币种由调用者验签及本层冻结值共同约束。
// 金额使用十进制精确比较，
// 不能按浮点容差接受少付。
// 相同密钥签出的其他渠道通知仍须与订单渠道完全一致。
// 到账事件与发票状态同事务，
// 任一写入失败都要求网关重试。
// 已经退款的历史订单收到重复通知不重新开启履约。
// 过期页面不能否认真实成功收款，
// 因此本函数不检查前端倒计时。
// 这里只接受外部支付，
// 余额付款必须走带额度检查的事务。
// 通知引用供内部对账，
// 不作为用户账户资料保存。
// 用户回跳页面不调用此函数，
// 不能凭浏览器参数冒充验签。
// 已付款重复调用不重新推进版本，
// 也不覆盖后续人工状态。
func CompleteAccountRechargePayment(tradeNo, provider, method, ref string, amount decimal.Decimal) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var o AccountRechargeOrder
		if err := lockForUpdate(tx).Where("trade_no = ?", tradeNo).First(&o).Error; err != nil {
			return err
		}
		if provider == "wallet" || o.PaymentProvider != provider || o.PaymentMethod != method || !amount.Equal(decimal.NewFromInt(o.MoneyCents).Div(decimal.NewFromInt(100))) {
			return ErrRechargeInvalid
		}
		if o.PaymentStatus == "paid" || o.PaymentStatus == "refunding" || o.PaymentStatus == "refunded" {
			return nil
		}
		if o.PaymentStatus != "pending" {
			return ErrRechargeConflict
		}
		o.PaymentStatus, o.ProviderRef, o.PaidAt = "paid", ref, common.GetTimestamp()
		if o.InvoiceRequired {
			if err := MarkInvoicePaidTx(tx, o.TradeNo); err != nil {
				return err
			}
		}
		return saveRechargeEvent(tx, &o, 0, "payment", "")
	})
}

// 缓存预留标记与余额放在同一hash，
// 过期时一起失效。
// 同单重复付款不会重复预留；
// 不能使用数据库余额绕过模型请求的预留。
// 缓存不可用直接停止付款，
// 不回退到可能尚未落库的余额。
// 退款先登记pending，
// 提交数据库后才允许幂等增加缓存余额。
// 标记以平台交易号区分扣款和退款，
// 不能跨动作复用。
// 预留直接操作模型调用正在使用的Quota 字段。
// 余额低于应付额度时不会创建成功标记。
// 重复扣款发现 done后不再修改缓存，
// 继续恢复数据库订单。
// 退款准备阶段不加额，
// 只记录后续允许同步的状态。
// 退款确认只对 pending加额，
// 重复确认遇到 done保持不变。
// 退款缓存加额前校验安全整数上限，
// 不截断金额。
// 事务失败时 release只撤销确实存在的扣款预留。
// 缓存整体过期后旧标记一同丢失，
// 不向冷缓存补造字段。
// 没有退款准备标记的热缓存可能来自已退款后的数据库水合。
// 该情况下不能再次补回额度，
// 否则会把退款重复加入缓存。
// 脚本中的整数上限与common.MaxWalletQuota 的存储契约一致。
// 操作同一 hash 避免多个Redis key跨槽事务依赖。
const rechargeQuotaScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[1])
 or tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0') ~= tonumber(ARGV[2])
 or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then return -1 end
local marker = redis.call('HGET', KEYS[1], ARGV[3])
local mode = ARGV[4]
local amount = tonumber(ARGV[5])
if mode == 'debit' then
 if marker == 'done' then return 1 end
 if tonumber(redis.call('HGET', KEYS[1], 'Quota')) < amount then return 0 end
 redis.call('HINCRBY', KEYS[1], 'Quota', -amount)
 redis.call('HSET', KEYS[1], ARGV[3], 'done')
elseif mode == 'prepare' then
 if not marker then redis.call('HSET', KEYS[1], ARGV[3], 'pending') end
elseif mode == 'credit' then
 if marker == 'pending' then
  if tonumber(redis.call('HGET', KEYS[1], 'Quota')) + amount > 9007199254740991 then return -2 end
  redis.call('HINCRBY', KEYS[1], 'Quota', amount)
  redis.call('HSET', KEYS[1], ARGV[3], 'done')
 end
elseif mode == 'release' and marker == 'done' then
 redis.call('HINCRBY', KEYS[1], 'Quota', amount)
 redis.call('HDEL', KEYS[1], ARGV[3])
end
return 1`

// 仅此处访问订单专用标记，
// 额度变更仍使用既有用户缓存。
// credit 遇到冷缓存不创建残缺hash，
// 下一次鉴权从已提交数据库水合。
// 关闭 Redis时数据库仍是资金账本，
// 不建立另一份进程余额。
// 扣款缓存缺失先走既有完整水合，
// 避免只创建 Quota残缺字段。
// 水合后的扣款仍重新执行原子脚本，
// 不能使用读取快照扣款。
// Redis请求失败返回可恢复错误，
// 不认为网络失败代表未扣款。
// credit 不主动从旧快照水合，
// 以免重复计入已提交的退款。
// prepare的冷缓存由后续鉴权从数据库恢复，
// 不强制保存空记录。
// 此函数不落数据库，
// 调用者必须把缓存动作放在正确事务阶段。
// 缓存同步结果不能用订单查询触发，
// 只有明确付款或退款操作才调用。
func rechargeCache(o *AccountRechargeOrder, mode string) error {
	if !common.RedisEnabled {
		return nil
	}
	field := "recharge:debit:" + o.TradeNo
	if mode == "prepare" || mode == "credit" {
		field = "recharge:refund:" + o.TradeNo
	}
	result, err := common.RDB.Eval(context.Background(), rechargeQuotaScript, []string{getUserCacheKey(o.UserID)}, o.UserID, userCacheSchemaVersion, field, mode, o.WalletQuota).Int()
	if err != nil {
		return ErrRechargeCache
	}
	if result == -1 && (mode == "debit" || mode == "prepare") {
		if _, err := GetUserCache(o.UserID); err != nil {
			return ErrRechargeCache
		}
		result, err = common.RDB.Eval(context.Background(), rechargeQuotaScript, []string{getUserCacheKey(o.UserID)}, o.UserID, userCacheSchemaVersion, field, mode, o.WalletQuota).Int()
	}
	if err != nil || (result == -1 && mode == "debit") {
		return ErrRechargeCache
	}
	if result == -2 {
		return ErrRechargeCache
	}
	if result == 0 {
		return ErrRechargeBalance
	}
	return nil
}

// 行锁串行化同单付款；
// DB扣款、已付状态与 payment事件同事务提交。
// Redis预留必须先成功，
// 才能消费不会与模型请求争抢的额度。
// 提交结果未知时不补回缓存；
// 用户用原订单恢复，
// 不能新建单猜测结果。
// 重复请求先锁订单再检查已付状态，
// 不能先减用户余额再找订单。
// 付款期间商品下架或关闭余额支付会阻止尚未成功的扣款。
// 用户状态在实际扣款条件中检查，
// 停用用户不能消费旧页面订单。
// 钱包字段更新使用 quota >=应付额度的条件，
// 不允许透支购买。
// 模型调用可能先预留后落库，
// 因此缓存余额检查必须早于数据库扣款。
// 数据库事件失败也回滚已付状态和余额，
// 避免缺少资金追溯记录。
// 缓存释放必须重新锁单确认，
// 不能补回另一并发请求已成功的扣款。
// 无法核实数据库提交结果时保持预留，
// 宁可明确待核对也不放大余额。
// 恢复使用原订单编号，
// 调用者不可生成新业务键掩盖不确定结果。
// 不使用钱包充值优惠、模型倍率或订阅剩余额度抵扣此商品。
func PayAccountRechargeWallet(id, userID int) error {
	var o AccountRechargeOrder
	var reserved bool
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("id = ? AND user_id = ?", id, userID).First(&o).Error; err != nil {
			return err
		}
		if o.PaymentProvider != "wallet" {
			return ErrRechargeInvalid
		}
		if o.PaymentStatus == "paid" {
			return nil
		}
		if o.PaymentStatus != "pending" || o.WalletQuota <= 0 {
			return ErrRechargeConflict
		}
		var p AccountRechargeProduct
		if err := tx.First(&p, o.ProductID).Error; err != nil {
			return err
		}
		if !p.Enabled || !p.AllowWalletPayment {
			return ErrRechargeUnavailable
		}
		if err := rechargeCache(&o, "debit"); err != nil {
			return err
		}
		reserved = true
		result := tx.Model(&User{}).Where("id = ? AND status = ? AND quota >= ?", userID, common.UserStatusEnabled, o.WalletQuota).Update("quota", gorm.Expr("quota - ?", o.WalletQuota))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRechargeBalance
		}
		o.PaymentStatus, o.PaidAt = "paid", common.GetTimestamp()
		return saveRechargeEvent(tx, &o, userID, "payment", "")
	})
	if err != nil && reserved {
		// 确认数据库仍待付后才补偿，
		// 未知提交结果保留预留供同单恢复。
		// 另取行锁，
		// 避免另一付款请求已提交而本请求错误释放其预留。
		reconcile := DB.Transaction(func(tx *gorm.DB) error {
			var current AccountRechargeOrder
			if e := lockForUpdate(tx).First(&current, id).Error; e != nil {
				return e
			}
			if current.PaymentStatus == "pending" {
				return rechargeCache(&current, "release")
			}
			return nil
		})
		if reconcile != nil {
			return ErrRechargeCache
		}
	}
	return err
}

// 退款分为批准和确认，
// 批准后停止业务员读取及办理。
// 微信确认必须填网关已退款凭证，
// 不发起虚构的上游退款请求。
// 余额退款使用原单整数额度，
// 汇率变化不会改变实际退还数量。
// 批准先改变支付状态，
// 从而停止读取、领取和标记完成。
// 管理员必须先核实外部办理已停止；
// 程序不会撤回外部人工动作。
// 微信退款确认必须具有原支付渠道的真实凭证号。
// 平台不提供无收款依据的手工成功按钮，
// 也不模拟网关退款接口。
// 余额退款与微信退款不能互换，
// 避免把余额提现成现金。
// 钱包容量不足时保留退款中，
// 不静默截断或标记已退款。
// 退款事件与余额返还同事务，
// 唯一版本避免重复记录资金动作。
// 余额缓存同步失败不回滚已提交退款，
// 而是保留明确恢复标志。
// 重复确认已退款订单只修复缓存，
// 不再次写入用户余额。
// 订单终态清除在线密文，
// 退款后的业务员不能继续办理。
// 未开具发票随退款终止；
// 已开具文件保留给财务处理，
// 不能直接删除。
// 公开说明与内部网关参考号分别存储，
// 用户无需理解供应商状态。
func RefundAccountRecharge(id, actor, version int, confirm bool, note, ref string) error {
	if strings.TrimSpace(note) == "" || len([]rune(note)) > 1000 || len(ref) > 128 {
		return ErrRechargeInvalid
	}
	var order AccountRechargeOrder
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).First(&order, id).Error; err != nil {
			return err
		}
		// 重复资金确认只同步缓存，
		// 不重复写数据库退款。
		if order.PaymentStatus == "refunded" && confirm {
			return nil
		}
		if order.Version != version {
			return ErrRechargeConflict
		}
		if !confirm {
			if order.PaymentStatus != "paid" || order.Status == "completed" {
				return ErrRechargeConflict
			}
			order.PaymentStatus, order.PublicNote = "refunding", note
			return saveRechargeEvent(tx, &order, actor, "refund_approve", note)
		}
		if order.PaymentStatus != "refunding" {
			return ErrRechargeConflict
		}
		if order.PaymentProvider == "wallet" {
			if order.WalletQuota <= 0 || common.ValidateWalletQuota(order.WalletQuota) != nil {
				return ErrRechargeInvalid
			}
			if err := rechargeCache(&order, "prepare"); err != nil {
				return err
			}
			result := tx.Model(&User{}).Where("id = ? AND quota <= ?", order.UserID, common.MaxWalletQuota-order.WalletQuota).Update("quota", gorm.Expr("quota + ?", order.WalletQuota))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrRechargeConflict
			}
		} else if strings.TrimSpace(ref) == "" {
			return ErrRechargeInvalid
		}
		order.PaymentStatus, order.Status, order.Secret = "refunded", "cancelled", ""
		order.WalletSyncPending = order.PaymentProvider == "wallet" && common.RedisEnabled
		// 未开具的发票随原支付退款终止，
		// 已开具发票保留供财务处理红冲。
		if order.InvoiceRequired {
			if err := tx.Model(&InvoiceRequest{}).Where("trade_no = ? AND status IN ?", order.TradeNo, []string{"pending", "processing"}).Updates(map[string]any{"status": "rejected", "remark": "Order refunded", "updated_at": common.GetTimestamp()}).Error; err != nil {
				return err
			}
		}
		order.RefundRef, order.PublicNote = ref, note
		return saveRechargeEvent(tx, &order, actor, "refund", note)
	})
	if err != nil {
		return err
	}
	if confirm && order.PaymentProvider == "wallet" {
		if err := rechargeCache(&order, "credit"); err != nil {
			return err
		}
		// 幂等同步标志只在缓存确认后清除；
		// 重试不会再次改变数据库余额。
		return DB.Model(&AccountRechargeOrder{}).Where("id = ? AND payment_status = ?", order.ID, "refunded").Update("wallet_sync_pending", false).Error
	}
	return nil
}
