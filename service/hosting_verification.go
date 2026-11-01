package service

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
)

// 挂靠敏感动作复用统一验证机制，不另发一套弱化验证的会话。
const VerificationScopeHostingManage = "hosting.manage"

// HostingManageContext 将证明限定到具体账号与消费 UUID。
// 绑定新账号使用 account_id=0；其他动作不得使用这个保留值。
type HostingManageContext struct {
	Action      string `json:"action"` // connect/reauthorize/delete/reset。
	AccountID   int    `json:"account_id"`
	OperationID string `json:"operation_id"` // 仅 reset 需要，必须是原操作的小写 UUID v4。
}

// normalizeHostingVerification 拒绝多余字段，避免证明和实际操作各自解释参数。
// 原厂账号归属在业务 API 中再检查；此处只负责精确绑定上下文。
// 此上下文参与证明摘要，字段集合必须确定且不能由业务入口另作解释。
// 所有动作都保留三个键，空 operation_id 也是明确约束而非缺省行为。
// connect 的账号零值只表示尚未创建，不授予任意已有账号的操作权限。
// reauthorize 和 delete 必须绑定正数账号，且不能携带重置操作身份。
// reset 必须绑定规范的小写 UUIDv4，避免等价文本形成不同证明摘要。
// 未知动作直接拒绝，不根据字符串前缀猜测动作或权限。
// 本函数不查询数据库，归属由实际业务入口在信任边界校验。
// 返回值是规范化结构，验证与消费共用同一实现。
// 失败不会创建或消费任何证明，调用者应返回上下文错误。
// 这套证明只授权管理动作，不能充当执行模型的 API Key。
func normalizeHostingVerification(raw json.RawMessage, fields map[string]json.RawMessage) (HostingManageContext, error) {
	var context HostingManageContext
	if len(fields) != 3 || common.Unmarshal(raw, &context) != nil {
		return context, ErrVerificationContextInvalid
	}
	// 字段必须完整出现，缺字段不能借默认零值通过证明校验。
	for _, key := range []string{"action", "account_id", "operation_id"} {
		if _, ok := fields[key]; !ok {
			return context, ErrVerificationContextInvalid
		}
	}
	switch context.Action {
	case "connect":
		if context.AccountID != 0 || context.OperationID != "" {
			return context, ErrVerificationContextInvalid
		}
	case "reauthorize", "delete":
		if context.AccountID <= 0 || context.OperationID != "" {
			return context, ErrVerificationContextInvalid
		}
	case "reset":
		// 相同账号的另一次消费仍需新证明，不能拿旧证明换 operation_id。
		id, err := uuid.Parse(context.OperationID)
		if context.AccountID <= 0 || err != nil || id.Version() != 4 || id.String() != context.OperationID {
			return context, ErrVerificationContextInvalid
		}
	default:
		return context, ErrVerificationContextInvalid
	}
	return context, nil
}
