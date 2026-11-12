package authz

// ResourceFinance 统一标识财务工作区，不复用渠道或系统设置权限。
// 开票资料和附件均受同一资源约束，用户自己的发票另由归属校验保护。
const ResourceFinance = "finance"

// FinanceRead 允许查看财务订单、开票申请、挂帐记录及受保护附件。
// 该能力不允许改价、补余额或管理用户。
var FinanceRead = Permission{Resource: ResourceFinance, Action: ActionRead} // 标签相同不代表与其它管理资源共享授权。

// FinanceWrite 允许处理开票申请、上传文件和登记挂帐整笔结清。
// 后端处理器仍须验证付款状态及申请生命周期。
var FinanceWrite = Permission{Resource: ResourceFinance, Action: ActionWrite}

// ResourceFinanceOverview 单独标识经营汇总，不随开票和挂帐办理权限一并授予。
// 汇总包含全平台收入与欠款，能办理单笔业务不代表可以查看整体经营数据。
const ResourceFinanceOverview = "finance_overview"

// FinanceOverviewRead 允许查看财务总览；没有任何角色默认拥有。
// Root 由 superuser 规则放行，其余账号必须由 Root 逐用户授予。
var FinanceOverviewRead = Permission{Resource: ResourceFinanceOverview, Action: ActionRead}

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
				LabelKey:       "View invoices and credit",
				DescriptionKey: "View invoices, credit balances and settlement records.",
				DefaultRoles:   []string{BuiltInRoleAdmin, BuiltInRoleFinance}, // 列表和文件下载都由读权限约束。
			},
			{
				Action:         ActionWrite,
				LabelKey:       "Manage invoices and credit",
				DescriptionKey: "Process invoices and confirm full credit settlements.",
				DefaultRoles:   []string{BuiltInRoleAdmin, BuiltInRoleFinance},
			},
		},
	})
	// 默认角色留空：管理员和财务都不会因升级自动看到经营汇总。
	RegisterResource(ResourceDefinition{
		Resource: ResourceFinanceOverview,
		LabelKey: "Financial overview",
		Actions: []ActionDefinition{
			{
				Action:         ActionRead,
				LabelKey:       "View financial overview",
				DescriptionKey: "View platform-wide revenue, receivables and wallet totals.",
			},
		},
	})
}
