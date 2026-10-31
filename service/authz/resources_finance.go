package authz

// ResourceFinance 统一标识财务工作区，不复用渠道或系统设置权限。
// 开票资料和附件均受同一资源约束，用户自己的发票另由归属校验保护。
const ResourceFinance = "finance"

// FinanceRead 允许查看财务订单、开票申请及受保护附件。
// 该能力不允许改价、补余额或管理用户。
var FinanceRead = Permission{Resource: ResourceFinance, Action: ActionRead} // 标签相同不代表与其它管理资源共享授权。

// FinanceWrite 允许处理开票申请和上传已开票文件。
// 后端处理器仍须验证付款状态及申请生命周期。
var FinanceWrite = Permission{Resource: ResourceFinance, Action: ActionWrite}

// 角色基线由后端目录唯一维护，前端通过现有权限目录获取。
// 普通管理员保留财务办理能力，财务角色仅获得这两个动作。
// Root 由现有 superuser 规则隐式拥有，不另存重复策略。
func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceFinance,
		LabelKey: "Finance",
		Actions: []ActionDefinition{
			{
				Action:         ActionRead,
				LabelKey:       "View invoice requests",
				DescriptionKey: "View paid orders, invoice requests and protected invoice files.",
				DefaultRoles:   []string{BuiltInRoleAdmin, BuiltInRoleFinance}, // 列表和文件下载都由读权限约束。
			},
			{
				Action:         ActionWrite,
				LabelKey:       "Process invoice requests",
				DescriptionKey: "Review invoice requests and upload issued invoice files.",
				DefaultRoles:   []string{BuiltInRoleAdmin, BuiltInRoleFinance},
			},
		},
	})
}
