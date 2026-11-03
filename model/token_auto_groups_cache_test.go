package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenAutoGroupsRoundTripThroughRedisHashCache(t *testing.T) {
	useUserCacheMiniRedis(t)
	token := Token{
		Id:         42,
		UserId:     7,
		Key:        "token-auto-groups-cache-key",
		Name:       "auto-cache",
		Group:      "auto",
		AutoGroups: `["vip","default"]`,
	}

	require.NoError(t, cacheSetTokenForTest(token))
	cached, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, token.AutoGroups, cached.AutoGroups)
	groups, err := cached.GetAutoGroups()
	require.NoError(t, err)
	assert.Equal(t, []string{"vip", "default"}, groups)
}

func TestTokenAllowUserQuotaQueryRoundTripsThroughRedisHashCache(t *testing.T) {
	server := useUserCacheMiniRedis(t)
	token := Token{Id: 43, UserId: 7, Key: "token-user-quota-cache-key", Name: "user-quota-cache", AllowUserQuotaQuery: true}

	initialized, err := cacheInitToken(token)
	require.NoError(t, err)
	require.Equal(t, 1, initialized)
	cached, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.True(t, cached.AllowUserQuotaQuery)
	assert.Positive(t, server.TTL(getTokenCacheKey(token.Key)))
}

func TestTokenUpdateSynchronouslyNarrowsPreheatedAutoGroupsCache(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	token := Token{
		UserId:          7,
		Key:             "token-auto-groups-update-cache-key",
		Name:            "auto-cache-update",
		Status:          common.TokenStatusEnabled,
		ExpiredTime:     -1,
		UnlimitedQuota:  true,
		Group:           "auto",
		CrossGroupRetry: true,
		AutoGroups:      `["default","vip"]`,
	}
	require.NoError(t, token.Insert())
	require.NoError(t, cacheSetTokenForTest(token))

	preheated, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.JSONEq(t, `["default","vip"]`, preheated.AutoGroups)

	require.NoError(t, token.SetAutoGroups([]string{"vip"}))
	require.NoError(t, token.Update())
	// Update 是限制性变更：写库前删除缓存并设置 fence。缓存不再提供旧的
	// 宽分组值，下一次读取必须看到收紧后的分组。
	_, cacheErr := cacheGetTokenByKey(token.Key)
	require.Error(t, cacheErr, "the pre-update cache entry must be invalidated")
	reloaded, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	assert.JSONEq(t, `["vip"]`, reloaded.AutoGroups)
}

// cacheSetTokenForTest 以测试身份写入完整 token 缓存（含额度字段），
// 模拟“已水合”的缓存状态。
func cacheSetTokenForTest(token Token) error {
	_, err := cacheInitToken(token)
	return err
}

// 映射必须在真实 Lua 初始化后往返，不能用通用 HSET fixture 绕过生产缓存字段表。
// 冷请求从数据库读到规则并不证明热请求也能读到；这里直接验证热缓存返回值。
// 使用固定测试密钥与规则，不读取生产认证资料。
func TestTokenModelMappingRoundTripsThroughRedisHashCache(t *testing.T) {
	useUserCacheMiniRedis(t)
	token := Token{Id: 44, UserId: 7, Key: "mapping-cache-fixture", ModelMapping: `{"gpt-6.1-terra":"kimi-k3"}`}
	// 冷缓存必须真正初始化；已有哈希可能掩盖 Lua 漏写字段的问题。
	initialized, err := cacheInitToken(token)
	require.NoError(t, err)
	require.Equal(t, 1, initialized)
	// 直接读取热缓存，排除数据库回源让测试错误通过的可能。
	cached, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	// 比较实际规则，不能仅检查字段存在；空规则同样会绕过映射。
	assert.Equal(t, token.ModelMapping, cached.ModelMapping)
	// 存量哈希不得被较早的数据库快照覆盖，即使旧快照没有映射规则。
	// 这与缓存额度不可被旧快照覆盖遵守同一初始化边界。
	token.ModelMapping = ""
	_, err = cacheInitToken(token)
	require.NoError(t, err)
	cached, err = cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, `{"gpt-6.1-terra":"kimi-k3"}`, cached.ModelMapping)
}

// 升级不能要求清空全部 Redis；旧哈希缺字段时应回源并只补齐映射元数据。
// 额度可能已经被并发请求扣减，修复映射时不能用数据库旧额度覆盖缓存。
// 通过公共读取入口验证回源后会修复缓存，而非只让当前请求偶然成功。
func TestTokenLegacyCacheRecoversMappingWithoutResettingQuota(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	// 数据库存保留原额度，缓存随后模拟已预扣的状态。
	// 两侧必须故意不一致，才能暴露补缺时错误覆盖余额的回归。
	token := Token{UserId: 7, Key: "legacy-mapping-fixture", RemainQuota: 100, ModelMapping: `{"gpt-6.1-terra":"kimi-k3"}`}
	require.NoError(t, token.Insert())
	_, err := cacheInitToken(token)
	require.NoError(t, err)
	key := getTokenCacheKey(token.Key)
	// 旧版本真实形状：存在令牌身份和额度，但没有 ModelMapping 成员。
	// 空字符串则是用户明确关闭映射，不能与字段缺失混为一谈。
	require.NoError(t, common.RDB.HDel(t.Context(), key, "ModelMapping").Err())
	require.NoError(t, common.RDB.HSet(t.Context(), key, "RemainQuota", 30).Err())
	_, err = cacheGetTokenByKey(token.Key)
	require.Error(t, err, "旧哈希不能伪装成未配置映射的有效缓存")
	// 正常请求无需知道缓存版本；回源取到的映射应立即可用。
	loaded, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	assert.Equal(t, token.ModelMapping, loaded.ModelMapping)
	// 下一次读取应直接得到完整缓存，不能永久回源刷新 TTL 却不修复字段。
	cached, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, token.ModelMapping, cached.ModelMapping)
	// 此处检查缓存真实余额，而非数据库返回的尚未同步余额。
	assert.Equal(t, 30, cached.RemainQuota)
	// 显式清空是有效热缓存，不能重新填入数据库仍保留的旧映射。
	require.NoError(t, common.RDB.HSet(t.Context(), key, "ModelMapping", "").Err())
	cached, err = cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Empty(t, cached.ModelMapping)
}

// 编辑和清空都必须跨过变更栅栏再验证，避免只覆盖回源而漏测热缓存。
// 同一令牌连续变更，复现用户保存后继续发请求的实际生命周期。
func TestTokenModelMappingUpdateAndClearSurviveCacheWarmup(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	token := Token{UserId: 7, Key: "mapping-update-fixture", ModelMapping: `{"alias":"initial"}`}
	require.NoError(t, token.Insert())
	require.NoError(t, cacheSetTokenForTest(token))
	for _, mapping := range []string{`{"alias":"kimi-k3"}`, ""} {
		// 保留编辑前快照，用它模拟在写库之前开始的并发读取。
		stale := token
		token.ModelMapping = mapping
		require.NoError(t, token.Update())
		// 栅栏期不允许旧规则重新发布，清空也属于同样的配置变更。
		// 这里只观察拒绝结果，不用等待真实时间制造偶发失败。
		initialized, err := cacheInitToken(stale)
		require.NoError(t, err)
		assert.Zero(t, initialized)
		// 栅栏期间读取仍走数据库，应立即看到用户刚保存的配置。
		loaded, err := GetTokenByKey(token.Key, false)
		require.NoError(t, err)
		assert.Equal(t, mapping, loaded.ModelMapping)
		// 推进 Redis 时间模拟栅栏自然过期，再通过公共入口预热。
		// 此后热读取也必须一致，否则会重现“保存时有效、稍后失效”。
		server.FastForward(time.Duration(tokenCacheFenceSeconds+1) * time.Second)
		_, err = GetTokenByKey(token.Key, false)
		require.NoError(t, err)
		// 直接命中缓存，确保更新或关闭不会只在回源期间生效。
		cached, err := cacheGetTokenByKey(token.Key)
		require.NoError(t, err)
		assert.Equal(t, mapping, cached.ModelMapping)
	}
}
