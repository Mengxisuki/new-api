package common

import (
	"errors"
	"strings"
	"unicode"

	"github.com/tidwall/gjson"
)

// TokenModelMapping 是每个 Key 独立的精确名称映射，不承担渠道别名转换。
// 保存和请求共用解析器，避免数据库直写的坏配置绕过循环检查。
// 源名称可以是客户端自定义别名，最终目标仍由现有权限和渠道可用性判断。
type TokenModelMapping map[string]string

// ErrTokenModelMapping 不携带用户提交的 JSON，避免错误响应泄露额外配置。
var ErrTokenModelMapping = errors.New("Invalid API key model mapping")

// ParseTokenModelMapping 限制配置大小、数量和名称，并拒绝歧义键与循环。
// 空字符串表示关闭；空对象也合法，null 和数组不是映射对象。
// 65535 字节与 256 项约束同时限制缓存体积和每次请求的解析成本。
// 字节上限兼容 MySQL TEXT，不能放行 65536 字节后只在某个方言写库失败。
// 输入保存为 TEXT，三个数据库不依赖各自的 JSON 运算符。
// 返回对象仅供当前调用读取，调用方不应共享后再并发修改。
// 不检查当前模型目录：管理员以后上下架模型不应改变配置是否可编辑。
// 可调用性与价格在请求时读取，保存映射本身不会预扣额度。
func ParseTokenModelMapping(raw string) (TokenModelMapping, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	if len(raw) > 65535 || !gjson.Valid(raw) {
		return nil, ErrTokenModelMapping
	}
	// 先验证完整文档，再读成员，拒绝尾随 JSON 或不完整草稿。
	// 解析错误只返回固定消息，不回显潜在误粘贴的凭据。
	root := gjson.Parse(raw)
	if !root.IsObject() {
		return nil, ErrTokenModelMapping
	}
	mapping := TokenModelMapping{}
	valid := true
	// gjson 逐成员遍历保留重复键，不能先解码到 map 后丢失歧义证据。
	// 值必须是字符串，不能把 null、数字或对象静默转换成模型名。
	root.ForEach(func(key, value gjson.Result) bool {
		source, target := key.String(), value.String()
		_, duplicate := mapping[source]
		if duplicate || len(mapping) >= 256 || value.Type != gjson.String || !validTokenModelName(source) || !validTokenModelName(target) {
			valid = false
			return false
		}
		// 不 TrimSpace 名称，避免把两个不同配置静默合并。
		// 保持用户填写的精确大小写，渠道层仍可应用自身的既有规范化。
		mapping[source] = target
		return true
	})
	if !valid {
		return nil, ErrTokenModelMapping
	}
	// 每条链均须终止；自映射作为明确不变配置保留，与渠道编辑体验一致。
	// 循环在保存时拒绝，请求时再次验证使坏存量配置失败可见。
	// 即使当前请求暂时用不到某条坏规则，也不能允许它进入有效配置。
	// 自环与多节点循环的区别在 Resolve 中集中处理。
	for source := range mapping {
		if _, err := mapping.Resolve(source); err != nil {
			return nil, err
		}
	}
	return mapping, nil
}

// validTokenModelName 保留供应商命名中的斜杠和冒号，不擅自归一化名称。
// 空白及控制字符会影响路径或日志解释，因此在配置入口拒绝。
// 名称上限与配置总长分别约束，超限不得截断为另一个模型。
func validTokenModelName(name string) bool {
	return name != "" && len(name) <= 256 && !strings.ContainsAny(name, "?#") && strings.IndexFunc(name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}

// Resolve 只精确匹配，返回链的最终目标；未命中保持客户端原名。
// 不做 reasoning 后缀猜测，避免用户未配置的模型被隐式改写。
// 有限映射最多经过 len(mapping) 个节点，多一步即证明存在环。
func (mapping TokenModelMapping) Resolve(source string) (string, error) {
	current := source
	for i := 0; i <= len(mapping); i++ {
		next, ok := mapping[current]
		if !ok || next == current {
			return current, nil
		}
		current = next
	}
	return "", ErrTokenModelMapping
}
