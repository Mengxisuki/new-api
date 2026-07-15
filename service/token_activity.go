package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/go-redis/redis/v8"
)

const (
	tokenActivityLeaseTTL        = 45 * time.Second
	tokenActivityHeartbeat       = 15 * time.Second
	tokenActivityIndexExpiration = 2 * time.Minute
)

type tokenActivityLease struct {
	userId    int
	tokenId   int
	expiresAt time.Time
}

var (
	tokenActivityMu       sync.Mutex
	tokenActivityLeases   = make(map[string]tokenActivityLease)
	tokenActivitySequence atomic.Uint64
)

func tokenActivityIndexKey(userId int) string {
	return fmt.Sprintf("token_activity:user:%d", userId)
}

func tokenActivityMember(tokenId int, requestId string) string {
	return fmt.Sprintf("%d:%s", tokenId, requestId)
}

func refreshTokenActivityLease(userId int, tokenId int, requestId string) {
	expiresAt := time.Now().Add(tokenActivityLeaseTTL)
	tokenActivityMu.Lock()
	tokenActivityLeases[requestId] = tokenActivityLease{
		userId:    userId,
		tokenId:   tokenId,
		expiresAt: expiresAt,
	}
	tokenActivityMu.Unlock()

	if !common.RedisEnabled || common.RDB == nil {
		return
	}
	gopool.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		pipeline := common.RDB.TxPipeline()
		indexKey := tokenActivityIndexKey(userId)
		pipeline.ZAdd(ctx, indexKey, &redis.Z{
			Score:  float64(expiresAt.Unix()),
			Member: tokenActivityMember(tokenId, requestId),
		})
		pipeline.Expire(ctx, indexKey, tokenActivityIndexExpiration)
		if _, err := pipeline.Exec(ctx); err != nil {
			common.SysError("failed to refresh token activity lease: " + err.Error())
		}
	})
}

func removeTokenActivityLease(userId int, tokenId int, requestId string) {
	tokenActivityMu.Lock()
	delete(tokenActivityLeases, requestId)
	tokenActivityMu.Unlock()

	if !common.RedisEnabled || common.RDB == nil {
		return
	}
	gopool.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := common.RDB.ZRem(
			ctx,
			tokenActivityIndexKey(userId),
			tokenActivityMember(tokenId, requestId),
		).Err(); err != nil {
			common.SysError("failed to remove token activity lease: " + err.Error())
		}
	})
}

func BeginTokenActivity(userId int, tokenId int, requestId string) func() {
	if userId <= 0 || tokenId <= 0 {
		return func() {}
	}
	if requestId == "" {
		requestId = fmt.Sprintf("local-%d-%d", time.Now().UnixNano(), tokenActivitySequence.Add(1))
	}

	ctx, cancel := context.WithCancel(context.Background())
	refreshTokenActivityLease(userId, tokenId, requestId)
	go func() {
		ticker := time.NewTicker(tokenActivityHeartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				refreshTokenActivityLease(userId, tokenId, requestId)
			case <-ctx.Done():
				return
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			removeTokenActivityLease(userId, tokenId, requestId)
		})
	}
}

func localActiveTokenCounts(userId int) map[int]int {
	now := time.Now()
	counts := make(map[int]int)
	tokenActivityMu.Lock()
	defer tokenActivityMu.Unlock()
	for requestId, lease := range tokenActivityLeases {
		if !lease.expiresAt.After(now) {
			delete(tokenActivityLeases, requestId)
			continue
		}
		if lease.userId == userId {
			counts[lease.tokenId]++
		}
	}
	return counts
}

func GetActiveTokenCounts(userId int) map[int]int {
	localCounts := localActiveTokenCounts(userId)
	if !common.RedisEnabled || common.RDB == nil {
		return localCounts
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	indexKey := tokenActivityIndexKey(userId)
	now := time.Now().Unix()
	pipeline := common.RDB.TxPipeline()
	pipeline.ZRemRangeByScore(ctx, indexKey, "-inf", strconv.FormatInt(now, 10))
	membersCmd := pipeline.ZRangeByScore(ctx, indexKey, &redis.ZRangeBy{
		Min: strconv.FormatInt(now+1, 10),
		Max: "+inf",
	})
	if _, err := pipeline.Exec(ctx); err != nil {
		common.SysError("failed to load token activity leases: " + err.Error())
		return localCounts
	}

	counts := make(map[int]int)
	for _, member := range membersCmd.Val() {
		parts := strings.SplitN(member, ":", 2)
		if len(parts) != 2 {
			continue
		}
		tokenId, err := strconv.Atoi(parts[0])
		if err == nil && tokenId > 0 {
			counts[tokenId]++
		}
	}
	for tokenId, count := range localCounts {
		if count > counts[tokenId] {
			counts[tokenId] = count
		}
	}
	return counts
}
