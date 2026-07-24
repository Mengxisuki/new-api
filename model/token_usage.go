package model

import (
	"fmt"
	"sort"

	"github.com/QuantumNous/new-api/common"
)

type TokenUsageKey struct {
	Id     int    `json:"id"`
	Name   string `json:"name"`
	Status int    `json:"status"`
}

type TokenUsagePoint struct {
	TokenId            int   `json:"token_id"`
	CreatedAt          int64 `json:"created_at"`
	SuccessfulRequests int64 `json:"successful_requests"`
	FailedRequests     int64 `json:"failed_requests"`
	PromptTokens       int64 `json:"prompt_tokens"`
	CompletionTokens   int64 `json:"completion_tokens"`
	Quota              int64 `json:"quota"`
	UseTimeSeconds     int64 `json:"use_time_seconds"`
	CacheReadTokens    int64 `json:"cache_read_tokens"`
	CacheCreateTokens  int64 `json:"cache_create_tokens"`
	LatencyMsSum       int64 `json:"latency_ms_sum"`
	LatencySamples     int64 `json:"latency_samples"`
}

type tokenUsageLogOther struct {
	CacheTokens            int64   `json:"cache_tokens"`
	CacheCreationTokens    int64   `json:"cache_creation_tokens"`
	CacheCreationTokens5m  int64   `json:"cache_creation_tokens_5m"`
	CacheCreationTokens1h  int64   `json:"cache_creation_tokens_1h"`
	CacheWriteTokens       int64   `json:"cache_write_tokens"`
	FirstResponseTimeMilli float64 `json:"frt"`
}

type tokenUsageBucket struct {
	TokenId   int
	CreatedAt int64
}

func GetUserTokenUsage(userId int, startTimestamp int64, endTimestamp int64) ([]TokenUsageKey, []TokenUsagePoint, error) {
	keys := make([]TokenUsageKey, 0)
	if err := DB.Model(&Token{}).
		Select("id, name, status").
		Where("user_id = ?", userId).
		Order("id ASC").
		Find(&keys).Error; err != nil {
		return nil, nil, err
	}

	rows, err := LOG_DB.Model(&Log{}).
		Select("token_id, token_name, created_at, type, quota, prompt_tokens, completion_tokens, use_time, other").
		Where("user_id = ? AND token_id > 0", userId).
		Where("created_at >= ? AND created_at <= ?", startTimestamp, endTimestamp).
		Where("type IN ?", []int{LogTypeConsume, LogTypeError}).
		Rows()
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	keyById := make(map[int]TokenUsageKey, len(keys))
	for _, key := range keys {
		keyById[key.Id] = key
	}
	pointsByBucket := make(map[tokenUsageBucket]*TokenUsagePoint)

	for rows.Next() {
		var tokenId int
		var tokenName string
		var createdAt int64
		var logType int
		var quota int64
		var promptTokens int64
		var completionTokens int64
		var useTimeSeconds int64
		var other string
		if err := rows.Scan(
			&tokenId,
			&tokenName,
			&createdAt,
			&logType,
			&quota,
			&promptTokens,
			&completionTokens,
			&useTimeSeconds,
			&other,
		); err != nil {
			return nil, nil, err
		}

		if _, ok := keyById[tokenId]; !ok {
			if tokenName == "" {
				tokenName = fmt.Sprintf("Deleted #%d", tokenId)
			}
			keyById[tokenId] = TokenUsageKey{Id: tokenId, Name: tokenName}
		}

		bucketTime := createdAt - createdAt%3600
		bucket := tokenUsageBucket{TokenId: tokenId, CreatedAt: bucketTime}
		point := pointsByBucket[bucket]
		if point == nil {
			point = &TokenUsagePoint{TokenId: tokenId, CreatedAt: bucketTime}
			pointsByBucket[bucket] = point
		}

		if logType == LogTypeError {
			point.FailedRequests++
			continue
		}

		point.SuccessfulRequests++
		point.Quota += quota
		point.PromptTokens += promptTokens
		point.CompletionTokens += completionTokens
		point.UseTimeSeconds += useTimeSeconds

		if other == "" {
			continue
		}
		otherInfo := tokenUsageLogOther{}
		if err := common.UnmarshalJsonStr(other, &otherInfo); err != nil {
			continue
		}
		point.CacheReadTokens += otherInfo.CacheTokens
		cacheCreateTokens := otherInfo.CacheCreationTokens
		if cacheCreateTokens == 0 {
			cacheCreateTokens = otherInfo.CacheCreationTokens5m + otherInfo.CacheCreationTokens1h
		}
		if cacheCreateTokens == 0 {
			cacheCreateTokens = otherInfo.CacheWriteTokens
		}
		point.CacheCreateTokens += cacheCreateTokens
		if otherInfo.FirstResponseTimeMilli > 0 {
			point.LatencyMsSum += int64(otherInfo.FirstResponseTimeMilli)
			point.LatencySamples++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	keys = keys[:0]
	for _, key := range keyById {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Id < keys[j].Id })

	points := make([]TokenUsagePoint, 0, len(pointsByBucket))
	for _, point := range pointsByBucket {
		points = append(points, *point)
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].CreatedAt != points[j].CreatedAt {
			return points[i].CreatedAt < points[j].CreatedAt
		}
		return points[i].TokenId < points[j].TokenId
	})
	return keys, points, nil
}
