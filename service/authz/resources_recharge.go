package authz

// 代充值订单与商品分开授权，
// 防止业务员改价或修改支付配置。
// Root 继续由统一superuser规则放行，
// 不维护重复授权记录。
const ResourceRechargeOrders = "recharge_orders"
const ResourceRechargeProducts = "recharge_products"

// 读取摘要不意味着允许读取账户凭据，
// 敏感动作必须单独授权。
var RechargeRead = Permission{Resource: ResourceRechargeOrders, Action: ActionRead}

// 办理权限还要在事务内检查领取人和订单状态。
var RechargeWrite = Permission{Resource: ResourceRechargeOrders, Action: ActionWrite}

// 敏感读取要求网页登录和单次验证，
// PAT 不开放。
var RechargeReveal = Permission{Resource: ResourceRechargeOrders, Action: "reveal"}

// 转派仅管理员拥有；
// 不改变付款状态。
var RechargeAssign = Permission{Resource: ResourceRechargeOrders, Action: "assign"}

// 退款独立于办理权限，
// 业务员不能执行资金动作。
var RechargeRefund = Permission{Resource: ResourceRechargeOrders, Action: "refund"}

// 商品读取与维护复用已有管理权限的读写约定。
var RechargeProductsRead = Permission{Resource: ResourceRechargeProducts, Action: ActionRead}
var RechargeProductsWrite = Permission{Resource: ResourceRechargeProducts, Action: ActionWrite}

// 基线随既有授权初始化加载；
// 业务员的权限硬边界在 Can中执行。
// 管理员可被逐用户撤销，
// 不能仅凭数字角色忽略授权矩阵。
func init() {
	RegisterResource(ResourceDefinition{Resource: ResourceRechargeOrders, LabelKey: "Recharge orders", Actions: []ActionDefinition{
		{Action: ActionRead, LabelKey: "View recharge orders", DefaultRoles: []string{BuiltInRoleAdmin, BuiltInRoleSales}},
		{Action: ActionWrite, LabelKey: "Process recharge orders", DefaultRoles: []string{BuiltInRoleAdmin, BuiltInRoleSales}},
		{Action: "reveal", LabelKey: "Reveal recharge account", DefaultRoles: []string{BuiltInRoleAdmin, BuiltInRoleSales}},
		{Action: "assign", LabelKey: "Assign recharge orders", DefaultRoles: []string{BuiltInRoleAdmin}},
		{Action: "refund", LabelKey: "Refund recharge orders", DefaultRoles: []string{BuiltInRoleAdmin}},
	}})
	// 商品与订单的权限配置独立，
	// 避免授予办理时一并授予定价权。
	RegisterResource(ResourceDefinition{Resource: ResourceRechargeProducts, LabelKey: "Recharge products", Actions: []ActionDefinition{
		{Action: ActionRead, LabelKey: "View recharge products", DefaultRoles: []string{BuiltInRoleAdmin}},
		{Action: ActionWrite, LabelKey: "Manage recharge products", DefaultRoles: []string{BuiltInRoleAdmin}},
	}})
}
