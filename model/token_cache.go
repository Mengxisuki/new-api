package model

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
)

func getTokenCacheKey(key string) string {
	return fmt.Sprintf("token:%s", common.GenerateHMAC(key))
}

func getTokenCacheFenceKey(key string) string {
	return fmt.Sprintf("token:fence:%s", common.GenerateHMAC(key))
}

func tokenCacheTTLSeconds() int {
	ttl := common.RedisKeyCacheSeconds()
	if ttl <= 0 {
		return 60
	}
	return ttl
}

// tokenCacheFenceSeconds must outlive a token mutation's database write plus
// any in-flight reader's DB-read-to-cache-init gap. The fence is not deleted
// after commit; it expires naturally so a reader holding a pre-mutation
// snapshot cannot publish it right after the mutation cleared the cache.
// While the fence exists readers simply serve the database without caching.
const tokenCacheFenceSeconds = 10

// invalidateTokenCacheForMutation is called before a token metadata mutation
// writes to the database: it raises the fence and drops the cached hash so no
// reader can act on (or re-publish) the pre-mutation state.
func invalidateTokenCacheForMutation(key string) error {
	if !common.RedisEnabled || key == "" {
		return nil
	}
	ctx := context.Background()
	err := common.RDB.Set(ctx, getTokenCacheFenceKey(key), 1, time.Duration(tokenCacheFenceSeconds)*time.Second).Err()
	if err != nil {
		return err
	}
	return common.RDB.Del(ctx, getTokenCacheKey(key)).Err()
}

// cacheInitToken 在无变更栅栏时初始化冷缓存，或补齐旧哈希缺失的映射。
// 已有字段一律保留：Redis 余额可能已经预扣，不能被数据库快照覆盖。
// 映射的空字符串表示明确关闭，只有字段缺失才允许回源补齐。
// token 必须来自数据库快照；此入口不负责验证或保存用户提交的规则。
// 正常配置编辑仍须先失效缓存，不能依赖补缺逻辑替换旧规则。
// 不更换缓存键或批量清缓存，避免升级丢失尚未同步到数据库的额度。
// 返回值：0=被栅栏拦截，1=完成初始化，2=已有哈希补缺并刷新 TTL。
func cacheInitToken(token Token) (int, error) {
	if !common.RedisEnabled {
		return 0, nil
	}
	allowIps := ""
	if token.AllowIps != nil {
		allowIps = *token.AllowIps
	}
	const script = `
-- 先检查变更栅栏；补缺与完整初始化都不能发布编辑前的快照。
if redis.call('EXISTS', KEYS[2]) == 1 then
  return 0
end
-- 旧版本遗漏映射字段，但其余额仍然有效，不能删除后重建。
-- HSETNX 区分字段缺失与显式空配置，也不会替换已有规则。
-- 这里只补元数据，令牌状态、模型限制与额度均沿用现有哈希。
if redis.call('EXISTS', KEYS[1]) == 1 then
  redis.call('HSETNX', KEYS[1], 'ModelMapping', ARGV[18])
  redis.call('EXPIRE', KEYS[1], ARGV[19])
  return 2
end
-- 冷哈希同时写入身份、限制、额度和映射，避免热请求丢失路由规则。
-- 即使未配置映射，也必须保存空值，作为完整缓存的标志。
redis.call('HSET', KEYS[1],
  'Id', ARGV[1], 'UserId', ARGV[2], 'Status', ARGV[3], 'Name', ARGV[4],
  'CreatedTime', ARGV[5], 'AccessedTime', ARGV[6], 'ExpiredTime', ARGV[7],
  'UnlimitedQuota', ARGV[8], 'ModelLimitsEnabled', ARGV[9], 'ModelLimits', ARGV[10],
  'AllowIps', ARGV[11], 'Group', ARGV[12], 'CrossGroupRetry', ARGV[13],
  'AutoGroups', ARGV[14], 'RemainQuota', ARGV[15], 'UsedQuota', ARGV[16],
  'AllowUserQuotaQuery', ARGV[17], 'ModelMapping', ARGV[18])
redis.call('EXPIRE', KEYS[1], ARGV[19])
return 1`

	// Lua 的映射参数为第 18 项，TTL 单独放在末尾；变更字段时须同步索引。
	// 初始化和补缺共用此脚本，保证与预扣以及令牌编辑之间的原子边界。
	// 两个键分别指向令牌哈希和编辑栅栏，使用同一密钥的 HMAC 定位。
	// 哈希中不写入原始密钥；认证成功后的返回值在读取端补回。
	return common.RDB.Eval(context.Background(), script, []string{
		getTokenCacheKey(token.Key), getTokenCacheFenceKey(token.Key),
	},
		token.Id, token.UserId, token.Status, token.Name,
		token.CreatedTime, token.AccessedTime, token.ExpiredTime,
		strconv.FormatBool(token.UnlimitedQuota), strconv.FormatBool(token.ModelLimitsEnabled),
		token.ModelLimits, allowIps, token.Group, strconv.FormatBool(token.CrossGroupRetry),
		token.AutoGroups, token.RemainQuota, token.UsedQuota,
		strconv.FormatBool(token.AllowUserQuotaQuery), token.ModelMapping,
		tokenCacheTTLSeconds(),
	).Int()
}

// cacheGetTokenByKey 从缓存读取 token；不完整的哈希（如仅有配额字段）会被拒绝。
// 缺失映射字段的旧缓存必须回源，不能当作用户未配置映射。
// 存在但为空的映射仍然有效，不会强制回源或恢复已关闭的规则。
// 回源和补缺由 GetTokenByKey 负责，本函数只判断缓存能否提供完整身份。
// 补齐映射不能使只有配额的残缺哈希成为有效令牌，仍须检查 Id。
func cacheGetTokenByKey(key string) (*Token, error) {
	if !common.RedisEnabled {
		return nil, fmt.Errorf("redis is not enabled")
	}
	var token Token
	// 字段存在性与解析使用同一次 HGETALL，避免两次读取间缓存被替换。
	if err := common.RedisHGetObj(getTokenCacheKey(key), &token, "ModelMapping"); err != nil {
		return nil, err
	}
	if token.Id <= 0 {
		return nil, fmt.Errorf("token cache is incomplete")
	}
	token.Key = key
	return &token, nil
}
