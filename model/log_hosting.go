package model

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// enrichHostingLogAccounts 为日志列表补充可识别的挂靠账号，不改写原始记录。
// 调用方已经完成日志可见性筛选，本函数不能扩大其分页范围。
// 自用、管理员和令牌列表复用同一规则，前端无需猜测查询权限。
// 返回错误后调用方中止本页输出，不能返回一部分补充成功的数据。
// 展示投影不是新数据库列，不需要迁移独立日志库。
// 导出仍保留原始日志合同，不把实时名称写进历史审计证据。
// 不使用全局身份缓存，改备注后再次读取列表即可看到新名称。
// 全流程不访问执行层，查看日志不会触发原厂查询或额外耗额。
// 账号名称来自当前备注和已存脱敏邮箱，不声称是请求发生时的名称快照。
// 已删除账号仍保留墓碑，查询不加 active 条件，避免历史身份消失。
// 同页引用批量查询，不能每条日志或每个回退步骤分别读取数据库。
// 主库与日志库可能独立，不能依赖跨库 JOIN 或特定数据库 JSON 操作。
// 只选显示所需列，凭据、内部 Key、原厂身份指纹与执行组不会进入内存投影。
// 账号 ID 不是授权依据，组装每条日志时再次匹配日志所属用户。
// 未找到或归属不符时不补名字，前端明确显示账号信息不可用。
// 原始原因码保留用于排障，用户界面另外翻译，不把本地语言存入历史字段。
// JSON 使用 RawMessage 保留未改字段精度，不通过 float64 重编码。
// 无相关字段的普通日志完全跳过，不让新功能依赖影响旧日志读取。
// 解析与数据库故障返回调用方，不能当作查询成功但账号不存在。
func enrichHostingLogAccounts(logs []*Log) error {
	// 请求范围同时按账号和所有者收集，限制批量查询能读取的身份集合。
	// 只取日志引用过的账号，不返回该用户其他已连接账号。
	ids := make(map[int]bool)
	owners := make(map[int]bool)
	references := make([][]int, len(logs))
	metadata := make([]map[string]json.RawMessage, len(logs))
	// 三个切片按同一页下标对应，不使用会被用户视图重编号的 Log.Id。
	// 这里只判断有无相关字段，字段类型仍交给下面的严格 JSON 解析。
	for i, entry := range logs {
		if !strings.Contains(entry.Other, `"hosting_account_id"`) && !strings.Contains(entry.Other, `"resource_skip_reasons"`) {
			continue
		}
		// 只解析稳定的历史引用合同；新展示信息由数据库重新生成。
		var refs struct {
			AccountID int      `json:"hosting_account_id"`
			Reasons   []string `json:"resource_skip_reasons"`
		}
		if err := common.UnmarshalJsonStr(entry.Other, &refs); err != nil {
			return err
		}
		// 实际来源可能是钱包，但其回退经过仍可能引用挂靠账号。
		// 因此同时收集实际使用账号和回退序列，不能只检查 resource_source。
		if refs.AccountID > 0 {
			references[i] = append(references[i], refs.AccountID)
		}
		// 平台原因中的零编号不是挂靠账号，不参与身份查询。
		// 原因格式不匹配只影响名称关联，不删除或重排历史尝试记录。
		for _, reason := range refs.Reasons {
			parts := strings.SplitN(reason, ":", 3)
			// 老版本或未知原因不含有效账号引用，仍保留原始内容供界面解释。
			if len(parts) != 3 || parts[0] != "hosting" {
				continue
			}
			id, err := strconv.Atoi(parts[1])
			if err == nil && id > 0 {
				references[i] = append(references[i], id)
			}
		}
		// 同一账号可能在实际来源和回退中重复出现，数据库只需读取一次。
		for _, id := range references[i] {
			ids[id] = true
		}
		if len(references[i]) == 0 {
			continue
		}
		owners[entry.UserId] = true
		if err := common.UnmarshalJsonStr(entry.Other, &metadata[i]); err != nil {
			return err
		}
	}
	if len(ids) == 0 {
		return nil
	}
	// 只从主库获取身份；日志后端为 ClickHouse 时也无需支持任何新列。
	accountIDs := make([]int, 0, len(ids))
	for id := range ids {
		accountIDs = append(accountIDs, id)
	}
	ownerIDs := make([]int, 0, len(owners))
	for owner := range owners {
		ownerIDs = append(ownerIDs, owner)
	}
	// 名称查询不锁行，日志展示不应阻塞账号暂停、改名或解绑。
	var accounts []HostingAccount
	if err := DB.Select("id", "user_id", "alias", "email_mask").Where("id IN ? AND user_id IN ?", accountIDs, ownerIDs).Find(&accounts).Error; err != nil {
		return err
	}
	// 数据库不保证 IN 查询结果顺序，用 ID 关联而不是按结果下标拼接。
	byID := make(map[int]HostingAccount, len(accounts))
	for _, account := range accounts {
		byID[account.ID] = account
	}
	for i, entry := range logs {
		if metadata[i] == nil {
			continue
		}
		// 每条日志拥有独立投影，管理员跨用户分页也不能混入别人的账号身份。
		display := make(map[int]map[string]string)
		for _, id := range references[i] {
			account, exists := byID[id]
			// 查询中的 owner 集合只限制整页范围，仍须逐条校验对应关系。
			// 管理员一页可能有多个所有者，不能仅因账号出现在查询结果中就公开。
			if exists && account.UserID == entry.UserId {
				display[id] = map[string]string{"alias": account.Alias, "email_mask": account.EmailMask}
			}
		}
		// 空映射同样覆盖旧显示字段，不能沿用未经本次归属校验的名字。
		// 序列化失败上抛，身份缺失与系统故障不能返回相同的成功结果。
		names, err := common.Marshal(display)
		if err != nil {
			return err
		}
		metadata[i]["hosting_accounts"] = names
		encoded, err := common.Marshal(metadata[i])
		if err != nil {
			return err
		}
		// 仅替换本次返回对象的 Other，数据库中的原文保持不变。
		entry.Other = string(encoded)
	}
	return nil
}
