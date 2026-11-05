package model

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 平台内部模块是唯一生产者；客户端不能自行向资金主题发布消息。
// 主题使用稳定标识，显示名称由控制台翻译，避免协议随界面语言变化。
const (
	HubTopicTopup    = "wallet.topup_succeeded"
	HubTopicCharge   = "billing.charge_anomaly"
	HubTopicRecharge = "recharge.info_submitted"
	HubTokenPrefix   = "evt_"
)

// 业务冲突和凭证拒绝使用稳定错误，不把驱动错误或认证原文返回客户端。
var (
	ErrHubInvalid   = errors.New("Invalid event push configuration")
	ErrHubForbidden = errors.New("Event push access denied")
	ErrHubConflict  = errors.New("Event push state changed. Refresh and try again.")
	hubCapture      atomic.Bool
)

// HubSettings 是部署级设置；关闭仅暂停投递，不影响业务事件持久化。
// 告警金额统一使用内部额度整数，不能与充值人民币分混用。
// 主键固定为一，设置更新与后台读取均使用主数据库。
type HubSettings struct {
	ID                int     `json:"-" gorm:"primaryKey"`                   // 固定单行，不按用户生成配置。
	Enabled           bool    `json:"enabled"`                               // 暂停时仍保留可恢复的通知。
	RetentionDays     int     `json:"retention_days"`                        // 到期记录按保留策略清理。
	AnomalyMinQuota   int     `json:"anomaly_min_quota" gorm:"type:bigint"`  // 历史异常的绝对门槛。
	AnomalyMultiplier float64 `json:"anomaly_multiplier"`                    // 相对中位数的倍率。
	AnomalyMinSamples int     `json:"anomaly_min_samples"`                   // 达到样本量才使用历史基线。
	AnomalyColdQuota  int     `json:"anomaly_cold_quota" gorm:"type:bigint"` // 冷启动独立高额阈值。
}

// HubEvent 只保存白名单业务摘要，绝不保存账户凭据或完整邮箱。
// 事件键标识真实业务动作，数据库唯一约束保障跨进程幂等。
// 自增编号仅用于标识；晚提交事件不能因编号较小而被游标跳过。
type HubEvent struct {
	ID        int64  `json:"id" gorm:"primaryKey;autoIncrement"`             // 对外稳定事件编号。
	Key       string `json:"-" gorm:"column:event_key;size:160;uniqueIndex"` // 真实订单/请求幂等键。
	Topic     string `json:"topic" gorm:"size:64;index"`                     // 事件类型由生产者确定。
	UserID    int    `json:"user_id" gorm:"index"`                           // 用于关注用户过滤。
	Payload   string `json:"-" gorm:"type:text"`                             // 仅存已序列化摘要。
	CreatedAt int64  `json:"created_at" gorm:"index"`                        // 保留期以实际业务记录时间计算。
}

// HubConsumer 是逻辑接收方，多个消费者各有完整的一份投递记录。
// 订阅与令牌分离，轮换凭证不会改变确认进度。
// 创建者必须保持启用且具备 Root 身份，降权立即撤销委托能力。
type HubConsumer struct {
	ExpiredUnackedCount int64  `json:"expired_unacked_count"` // 保留期清理未确认通知时累计，提醒业务对账。
	ID                  int    `json:"id" gorm:"primaryKey"`  // 连接通过明确消费者编号定位。
	OwnerID             int    `json:"-" gorm:"index"`        // 授权由服务端创建者持有。
	AuthVersion         int64  `json:"-"`                     // 密码重置等认证版本变化使旧授权失效。
	Name                string `json:"name" gorm:"size:64"`   // 显示用途，不作为认证依据。
	Topics              string `json:"-" gorm:"type:text"`    // 规范化主题集合。
	UserIDs             string `json:"-" gorm:"type:text"`    // 空集合代表全部已授权用户。
	Enabled             bool   `json:"enabled"`               // 禁用同时阻止新连接和继续投递。
	CreatedAt           int64  `json:"created_at"`            // 默认从创建时间关注新事件。
	LastSeenAt          int64  `json:"last_seen_at"`          // 最近成功验证客户端时间。
}

// HubToken 仅允许事件消费，不能进入通用后台或模型鉴权。
// 原文创建时仅返回一次，存储复用个人令牌的高熵哈希方式。
// 每枚令牌独立约束主题，扩大消费者订阅不会扩大已有令牌权限。
type HubToken struct {
	ID         int    `json:"id" gorm:"primaryKey"`         // 撤销必须同时匹配所属消费者。
	ConsumerID int    `json:"consumer_id" gorm:"index"`     // 不跨消费者共享确认进度。
	Name       string `json:"name" gorm:"size:64"`          // 标注客户端或轮换用途。
	TokenHash  string `json:"-" gorm:"size:64;uniqueIndex"` // 只保存不可用作凭证的摘要。
	TokenHint  string `json:"token_hint" gorm:"size:8"`     // 便于识别而不泄露原文。
	Topics     string `json:"-" gorm:"type:text"`           // 不能由连接参数扩大。
	ExpiresAt  int64  `json:"expires_at"`                   // 必须有明确有效期。
	CreatedAt  int64  `json:"created_at"`                   // 用于审计与轮换。
	LastUsedAt int64  `json:"last_used_at"`                 // 在线重检同步更新。
	RevokedAt  int64  `json:"revoked_at"`                   // 保留撤销记录供管理页查看。
}

// HubTopics 校验协议入口；空主题、未知主题均显式拒绝。
// 排序与去重使安全证明、订阅和令牌比较共享同一合同。
// 主题上限等于本期实际支持的三种事件。
// 调用者不能使用通配符请求尚未获得授权的未来主题。
// 返回新切片，避免规范化过程修改正在使用的请求快照。
func HubTopics(topics []string) ([]string, error) {
	if len(topics) == 0 || len(topics) > 3 {
		return nil, ErrHubInvalid
	}
	topics = slices.Clone(topics)
	for _, topic := range topics {
		if topic != HubTopicTopup && topic != HubTopicCharge && topic != HubTopicRecharge {
			return nil, ErrHubInvalid
		}
	}
	slices.Sort(topics)
	return slices.Compact(topics), nil
}

// HubTopicList 解码内部可信结构；坏数据必须让权限检查失败关闭。
// 返回错误使存储损坏可定位，不能解释成允许全部主题。
// 内部序列化格式同样需要通过主题目录验证。
// 移除或损坏的主题必须显式迁移，不能自动忽略后继续授权。
func HubTopicList(raw string) ([]string, error) {
	var topics []string
	if err := common.UnmarshalJsonStr(raw, &topics); err != nil {
		return nil, err
	}
	return HubTopics(topics)
}

// HubUserIDs 限制过滤大小，拒绝非正用户编号，空数组才表示不限用户。
// 过滤器只减少接收范围，不能扩大主题授权。
// 用户编号是平台公开标识，不是认证主体。
// 不在此查询用户是否存在，已删除用户的历史通知仍可保留。
// 集合排序便于控制台编辑后稳定回显，不依赖提交顺序。
func HubUserIDs(ids []int) ([]int, error) {
	if len(ids) > 100 {
		return nil, ErrHubInvalid
	}
	ids = slices.Clone(ids)
	for _, id := range ids {
		if id <= 0 {
			return nil, ErrHubInvalid
		}
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if ids == nil {
		ids = []int{}
	}
	return ids, nil
}

// ValidateHubSettings 在配置入口建立数值约束，检测器无需重复猜测默认值。
// 中位数最多使用一百笔，样本门槛必须在这个范围内。
// 额度限制沿用单请求整数范围，避免比较时出现无界数值。
// 倍率不允许NaN或无穷大，不能用特殊浮点值关闭规则。
// 保留期不是投递承诺，超过窗口的未确认消息会形成可见缺口。
// 设置改变只影响后续检测，不重写已经发出的历史告警。
func ValidateHubSettings(s HubSettings) error {
	if s.RetentionDays < 1 || s.RetentionDays > 365 || s.AnomalyMinSamples < 1 || s.AnomalyMinSamples > 100 || s.AnomalyMultiplier <= 1 || s.AnomalyMultiplier > 1000 || math.IsNaN(s.AnomalyMultiplier) || math.IsInf(s.AnomalyMultiplier, 0) || s.AnomalyMinQuota < 1 || s.AnomalyColdQuota < 1 || s.AnomalyMinQuota > math.MaxInt32 || s.AnomalyColdQuota > math.MaxInt32 {
		return ErrHubInvalid
	}
	return nil
}

// MigrateEventHub 同时用于启动和真实三库升级验证；重复执行保留原设置。
// 显式初始化业务默认值，避免不同数据库反复改写布尔默认。
// 与业务主库共同迁移，不能在独立日志库创建。
// 外部数据库使用相同GORM定义，不引入特定方言的JSON字段。
// 重复启动只插入缺失的默认行，不覆盖管理员暂停状态。
// 迁移本身不启用捕获，启动者需在全部表就绪后显式开启。
func MigrateEventHub(db *gorm.DB) error {
	if err := db.AutoMigrate(&HubSettings{}, &HubEvent{}, &HubConsumer{}, &HubToken{}, &HubDelivery{}, &HubConnection{}, &HubChargeObservation{}); err != nil {
		return err
	}
	defaults := HubSettings{ID: 1, Enabled: false, RetentionDays: 7, AnomalyMinQuota: 2500000, AnomalyMultiplier: 5, AnomalyMinSamples: 20, AnomalyColdQuota: 10000000}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&defaults).Error
}

// 捕获就绪由主库迁移成功后设置，不依赖可关闭的消费日志。
// 测试显式启用，旧业务夹具不需要额外构造无关事件表。
// 此开关表达存储就绪，不是业务配置项。
// 不提供HTTP修改入口，不能用它绕过正式事件捕获。
func SetHubCaptureEnabled(enabled bool) { hubCapture.Store(enabled) }
func HubCaptureEnabled() bool           { return hubCapture.Load() }

// GetHubSettings 每次读取主库，避免多节点设置缓存不同步。
// 多节点不能依赖本地默认值推断投递是否暂停。
// 读取失败由传输层报告服务不可用，不降级成默认放行。
func GetHubSettings() (HubSettings, error) {
	var s HubSettings
	err := DB.First(&s, 1).Error
	return s, err
}

// UpdateHubSettings 更新唯一设置行；零值布尔必须真实写入。
// 保存完整快照使零值布尔保持明确语义。
// 设置表没有消费者凭据，更新不影响Token摘要和确认进度。
// 不在这里清理旧数据，避免保存设置触发无界删除。
func UpdateHubSettings(s HubSettings) error {
	if err := ValidateHubSettings(s); err != nil {
		return err
	}
	s.ID = 1
	return DB.Save(&s).Error
}

// AppendHubEventTx 与业务更新共享事务；错误必须使业务写入回滚。
// 幂等重投只接受同一业务摘要，避免键复用悄悄覆盖不同资金动作。
// 调用者必须传入正在更新订单或账务的同一事务。
// 不能在返回成功后另起goroutine补写，否则进程退出会漏事件。
// 唯一键冲突后的摘要比较用于发现业务身份错误复用。
// 重复通知不会重写原始发生时间，也不会延长保留期。
// 本函数不触发网络投递，慢客户端不进入资金事务。
// 主键由数据库生成，不把原厂交易号直接当公开事件编号。
func AppendHubEventTx(tx *gorm.DB, key, topic string, userID int, payload any) error {
	if !HubCaptureEnabled() {
		return nil
	}
	if key == "" || len(key) > 160 || userID <= 0 {
		return ErrHubInvalid
	}
	if _, err := HubTopics([]string{topic}); err != nil {
		return err
	}
	encoded, err := common.Marshal(payload)
	if err != nil {
		return err
	}
	e := HubEvent{Key: key, Topic: topic, UserID: userID, Payload: string(encoded), CreatedAt: common.GetTimestamp()}
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&e)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	var existing HubEvent
	if err := tx.Where("event_key = ?", key).First(&existing).Error; err != nil {
		return err
	}
	if existing.Topic != topic || existing.UserID != userID || existing.Payload != e.Payload {
		return ErrHubConflict
	}
	return nil
}

// 消费者建立时锁定创建者，限制数量并冻结认证版本。
// 名称不用于权限判定，后续修改不改变所有者。
// 消费者数量在全平台限制为一百，后台扫描保持有界。
// 冻结AuthVersion让发行者重置认证资料后旧委托失效。
// 创建者来自控制器认证上下文，外部请求不能传入任意OwnerID。
// 空用户过滤在存储中明确保存为空数组，避免null语义分叉。
// 初始订阅仅从创建时间开始，历史补发需显式选择事件。
// 业务事件与消费者创建可能在同一秒发生，仍按持久时间筛选。
func CreateHubConsumer(ownerID int, name string, topics []string, userIDs []int) (*HubConsumer, error) {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 64 {
		return nil, ErrHubInvalid
	}
	topics, err := HubTopics(topics)
	if err != nil {
		return nil, err
	}
	ids, err := HubUserIDs(userIDs)
	if err != nil {
		return nil, err
	}
	topicJSON, err := common.Marshal(topics)
	if err != nil {
		return nil, err
	}
	idsJSON, err := common.Marshal(ids)
	if err != nil {
		return nil, err
	}
	c := HubConsumer{OwnerID: ownerID, Name: name, Topics: string(topicJSON), UserIDs: string(idsJSON), Enabled: true, CreatedAt: common.GetTimestamp()}
	err = DB.Transaction(func(tx *gorm.DB) error {
		var owner User
		if err := lockForUpdate(tx).Select("id", "role", "status", "auth_version").First(&owner, ownerID).Error; err != nil {
			return err
		}
		if owner.Role != common.RoleRootUser || owner.Status != common.UserStatusEnabled {
			return ErrHubForbidden
		}
		var n int64
		if err := tx.Model(&HubConsumer{}).Count(&n).Error; err != nil {
			return err
		}
		if n >= 100 {
			return fmt.Errorf("%w: consumer limit", ErrHubInvalid)
		}
		c.AuthVersion = owner.AuthVersion
		return tx.Create(&c).Error
	})
	return &c, err
}

// GetHubConsumer 返回持久状态，不使用客户端自报的所有者。
// 调用者负责提供已经校验的正编号。
// 不存在记录保留GORM错误供HTTP层映射404。
func GetHubConsumer(id int) (*HubConsumer, error) {
	var c HubConsumer
	err := DB.First(&c, id).Error
	return &c, err
}

// UpdateHubConsumer 不允许转移所有权；更改关注范围立即影响后续发送和补发。
// 旧投递仍保留作为审计，但不能借旧订阅读取已移除主题。
// 订阅编辑不会自动清除已有投递账本。
// 禁用是可逆操作，重新启用仍按原未确认记录恢复。
// 授权版本与所有者不在可更新字段中，不能借编辑刷新失效委托。
// MySQL相同值更新可能返回零行，需区分未变化和资源不存在。
// 新增主题的历史范围仍受消费者创建时间和保留期限制。
func UpdateHubConsumer(id int, name string, topics []string, ids []int, enabled bool) error {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 64 {
		return ErrHubInvalid
	}
	topics, err := HubTopics(topics)
	if err != nil {
		return err
	}
	ids, err = HubUserIDs(ids)
	if err != nil {
		return err
	}
	a, err := common.Marshal(topics)
	if err != nil {
		return err
	}
	b, err := common.Marshal(ids)
	if err != nil {
		return err
	}
	result := DB.Model(&HubConsumer{}).Where("id = ?", id).Updates(map[string]any{"name": name, "topics": string(a), "user_ids": string(b), "enabled": enabled})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		_, err = GetHubConsumer(id)
	}
	return err
}

// CreateHubToken 的随机源与指纹沿用个人令牌实现，原文不写数据库。
// 创建时再次验证消费者订阅，安全证明之外仍需落实资源边界。
// 令牌最多存活一年左右，控制台默认三十天。
// 每消费者最多二十枚活跃令牌，撤销和到期不占用活跃额度。
// 生成失败不得返回任何可用原文，事务成功后才交给调用者。
// 随机串只在当前栈中存在，后续列表无法找回完整凭证。
// 主题是消费者授权的子集，不能通过另建令牌绕过订阅限制。
// 命名只用于审计和识别轮换批次，不参与指纹计算。
func CreateHubToken(consumerID int, name string, topics []string, expiresAt int64) (string, *HubToken, error) {
	name = strings.TrimSpace(name)
	now := common.GetTimestamp()
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 64 || expiresAt < now+3600 || expiresAt > now+366*86400 {
		return "", nil, ErrHubInvalid
	}
	topics, err := HubTopics(topics)
	if err != nil {
		return "", nil, err
	}
	secret, err := common.GenerateRandomCharsKey(43)
	if err != nil {
		return "", nil, err
	}
	raw := HubTokenPrefix + secret
	encoded, err := common.Marshal(topics)
	if err != nil {
		return "", nil, err
	}
	token := HubToken{ConsumerID: consumerID, Name: name, TokenHash: AccessTokenFingerprint(raw), TokenHint: AccessTokenHint(raw), Topics: string(encoded), ExpiresAt: expiresAt, CreatedAt: now}
	err = DB.Transaction(func(tx *gorm.DB) error {
		var c HubConsumer
		if err := lockForUpdate(tx).First(&c, consumerID).Error; err != nil {
			return err
		}
		allowed, err := HubTopicList(c.Topics)
		if err != nil {
			return err
		}
		for _, topic := range topics {
			if !slices.Contains(allowed, topic) {
				return ErrHubForbidden
			}
		}
		var count int64
		if err := tx.Model(&HubToken{}).Where("consumer_id = ? AND revoked_at = 0 AND expires_at > ?", consumerID, now).Count(&count).Error; err != nil {
			return err
		}
		if count >= 20 {
			return ErrHubInvalid
		}
		return tx.Create(&token).Error
	})
	if err != nil {
		return "", nil, err
	}
	return raw, &token, nil
}

// RevokeHubToken 保留历史记录；所有节点会在下次投递前重查撤销状态。
// 重复撤销同一令牌是幂等成功。
// 错误的消费者编号不能撤销其他消费者的凭证。
// 不删除历史行，最后使用时间仍可用于定位接入来源。
func RevokeHubToken(consumerID, tokenID int) error {
	result := DB.Model(&HubToken{}).Where("id = ? AND consumer_id = ? AND revoked_at = 0", tokenID, consumerID).Update("revoked_at", common.GetTimestamp())
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var token HubToken
		return DB.Where("id = ? AND consumer_id = ?", tokenID, consumerID).First(&token).Error
	}
	return nil
}

// AuthenticateHubToken 每次都查主库，同时检查发行者状态与认证版本。
// 令牌格式校验在查库前完成；不接受URL凭证，也不回退到管理Token。
// 事件令牌采用独立前缀，避免与模型Key混用。
// 查询摘要而非原文，数据库泄露不会直接给出连接密钥。
// 消费者暂停、令牌过期和发行者失效都在主库检查。
// 返回的是当前订阅与令牌授权的交集，而非任意一方全集。
// 鉴权函数没有副作用，SSE可在每轮发送前安全重用。
func AuthenticateHubToken(raw string) (*HubConsumer, *HubToken, []string, error) {
	if !strings.HasPrefix(raw, HubTokenPrefix) || len(raw) != len(HubTokenPrefix)+43 {
		return nil, nil, nil, ErrHubForbidden
	}
	var token HubToken
	if err := DB.Where("token_hash = ? AND revoked_at = 0 AND expires_at > ?", AccessTokenFingerprint(raw), common.GetTimestamp()).First(&token).Error; err != nil {
		return nil, nil, nil, err
	}
	c, err := GetHubConsumer(token.ConsumerID)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := ValidateHubOwner(c); err != nil {
		return nil, nil, nil, err
	}
	own, err := HubTopicList(c.Topics)
	if err != nil {
		return nil, nil, nil, err
	}
	grant, err := HubTopicList(token.Topics)
	if err != nil {
		return nil, nil, nil, err
	}
	topics := []string{}
	for _, topic := range grant {
		if slices.Contains(own, topic) {
			topics = append(topics, topic)
		}
	}
	if len(topics) == 0 {
		return nil, nil, nil, ErrHubForbidden
	}
	return c, &token, topics, nil
}

// ValidateHubOwner 防止账号禁用、降权或凭证重置后保留旧平台委托。
// 重新启用不能自行刷新认证版本，管理员需要创建新的消费者授权。
// 发行者角色必须仍为Root，消费者不是永久平台权限副本。
// 普通消费者客户端不能重新绑定发行者或修改认证版本。
// 角色或账号状态未知时失败关闭，不继续发送摘要。
func ValidateHubOwner(c *HubConsumer) error {
	if !c.Enabled {
		return ErrHubForbidden
	}
	var owner User
	if err := DB.Select("id", "role", "status", "auth_version").First(&owner, c.OwnerID).Error; err != nil {
		return err
	}
	if owner.Role != common.RoleRootUser || owner.Status != common.UserStatusEnabled || owner.AuthVersion != c.AuthVersion {
		return ErrHubForbidden
	}
	return nil
}

// InitializeHubCapture 只在真实进程启动时启用捕获，数据库打开与运行时分离。
// 主节点负责迁移，从节点必须看到完整schema后才能接收业务流量。
// 未完成升级时明确拒绝启动，不能静默变成无通知节点。
// 独立测试夹具可显式启用捕获，不受其他测试是否执行迁移影响。
// 设置行读取失败同样阻止启动，避免后台在坏状态下不断重试。
func InitializeHubCapture() error {
	for _, table := range []any{&HubSettings{}, &HubEvent{}, &HubDelivery{}, &HubConsumer{}, &HubToken{}, &HubConnection{}, &HubChargeObservation{}} {
		if !DB.Migrator().HasTable(table) {
			return fmt.Errorf("event hub schema is not ready; migrate the master node first")
		}
	}
	if _, err := GetHubSettings(); err != nil {
		return err
	}
	SetHubCaptureEnabled(true)
	return nil
}
