package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// RechargeSetting 只控制是否接受新购买，不保存账户凭据或加密密钥。
// 关闭后已付款订单仍可提交资料、办理、退款及接收支付通知。
// 逐商品余额开关保持独立，不由此处覆盖。
type RechargeSetting struct {
	Enabled bool `json:"enabled"` // 实际可购买还需资料加密及支付配置就绪。
}

// 保持升级前已配置服务的接单行为，避免升级无故暂停营业。
// 新安装没有加密密钥时，即使此开关为真也不能下单。
var rechargeSetting = RechargeSetting{Enabled: true}

func init() {
	// 复用系统选项表和刷新机制，不新增数据库表或环境变量开关。
	config.GlobalConfig.Register("recharge_setting", &rechargeSetting)
}

// GetRechargeSetting 供购买入口读取管理员保存的接单意图。
// 运行时就绪状态不持久化，避免旧状态掩盖密钥丢失。
func GetRechargeSetting() *RechargeSetting {
	return &rechargeSetting
}
