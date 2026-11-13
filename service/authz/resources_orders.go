package authz

// 统一订单管理只对管理员默认开放，不扩大财务或业务员现有权限。
// 列表与退款分开授权；隐藏菜单不能代替每次HTTP请求的能力校验。
// 订单管理属于全平台交易操作。
// 财务现有权限不自动扩展为外部资金退款。
// 菜单显示和HTTP写入使用同一资源标识。
// 读取授权可以独立于退款授权授予。

const ResourceOrders = "orders"

var OrdersRead = Permission{Resource: ResourceOrders, Action: ActionRead}
var OrdersRefund = Permission{Resource: ResourceOrders, Action: "refund"}

// 普通管理员可办理退款，Root沿用原有全权规则。
// 退款是外部资金动作，不借用财务只读或代充值办理权限。
func init() {
	RegisterResource(ResourceDefinition{Resource: ResourceOrders, LabelKey: "Order management", Actions: []ActionDefinition{
		{Action: ActionRead, LabelKey: "View payment orders", DescriptionKey: "View wallet, subscription and recharge payment orders.", DefaultRoles: []string{BuiltInRoleAdmin}},
		{Action: "refund", LabelKey: "Refund payment orders", DescriptionKey: "Submit full WeChat refunds and recover their results.", DefaultRoles: []string{BuiltInRoleAdmin}},
	}})
}
