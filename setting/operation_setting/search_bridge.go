package operation_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// SearchBridgeRoutesOptionKey 复用系统选项持久化，不另存凭据或新增表。
// 空对象关闭桥接；键为平台计费目标，值为具有真实搜索能力的模型。
// 精确匹配避免把供应商新增模型自动纳入尚未验证的兼容范围。
const SearchBridgeRoutesOptionKey = "WebSearchBridgeRoutes"

// ParseSearchBridgeRoutes 在管理入口及运行时使用同一校验合同。
// 运行时重验历史直写数据，坏配置必须可见失败，不能静默关闭搜索。
// 当前只开放已经验证普通工具能力的两个目标，不按名称前缀猜测能力。
// 搜索后端必须使用 GPT 平台模型；其实际可用性由渠道和 Key 权限决定。
// 不接受链式映射，内部搜索调用直接走原生 Responses，不再递归桥接。
// 返回独立映射，管理员更新配置不会影响已经开始的请求。
func ParseSearchBridgeRoutes(raw string) (map[string]string, error) {
	if raw == "" {
		raw = "{}"
	}
	routes := map[string]string{} // 新建快照，解析失败不能留下半份可执行配置。
	if err := common.UnmarshalJsonStr(raw, &routes); err != nil || routes == nil {
		return nil, common.NewMessage("Search bridge routes must be a JSON object")
	}
	for target, backend := range routes {
		if (target != "kimi-k3" && target != "glm-5.3") || !strings.HasPrefix(backend, "gpt-") || len(backend) <= 4 || len(backend) > 128 || strings.ContainsAny(backend, " \t\r\n") {
			return nil, common.NewMessage("Search bridge routes require kimi-k3 or glm-5.3 targets and a GPT search model")
		}
	}
	return routes, nil
}

// SearchBridgeBackend 从受锁保护的系统选项取得本次配置快照。
// 仅返回模型标识，渠道地址、凭据与定价继续由现有系统管理。
// 不维护第二份缓存，关闭配置后下一请求立即恢复原生链路。
func SearchBridgeBackend(target string) (string, error) {
	// 未纳入适配的模型完全沿用原生请求，配置异常不扩大影响范围。
	if target != "kimi-k3" && target != "glm-5.3" {
		return "", nil
	}
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap[SearchBridgeRoutesOptionKey]
	common.OptionMapRWMutex.RUnlock()
	routes, err := ParseSearchBridgeRoutes(raw)
	return routes[target], err
}
