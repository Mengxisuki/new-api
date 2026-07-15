package model

import (
	"sort"
)

const TokenActivityOfficeLimit = 6

var officeCharacterSet = map[string]struct{}{
	"misaki":   {},
	"erikari":  {},
	"nate":     {},
	"harri":    {},
	"luke":     {},
	"soeren":   {},
	"mario":    {},
	"sinisa":   {},
	"spineboy": {},
}

type TokenActivityItem struct {
	Id               int    `json:"id"`
	Name             string `json:"name"`
	Status           int    `json:"status"`
	OfficeCharacter  string `json:"office_character"`
	ActiveRequests   int    `json:"active_requests" gorm:"-"`
	Requests24h      int    `json:"requests_24h" gorm:"-"`
	AccessedTime     int64  `json:"last_used_at"`
	CreatedTime      int64  `json:"-"`
	ResolvedActivity string `json:"state" gorm:"-"`
}

func IsValidOfficeCharacter(character string) bool {
	_, ok := officeCharacterSet[character]
	return ok
}

func GetUserTokenActivity(userId int, activeRequests map[int]int, since int64) ([]TokenActivityItem, error) {
	items := make([]TokenActivityItem, 0)
	if err := DB.Model(&Token{}).
		Select("id, name, status, office_character, accessed_time, created_time").
		Where("user_id = ?", userId).
		Find(&items).Error; err != nil {
		return nil, err
	}

	type requestCount struct {
		TokenId int `gorm:"column:token_id"`
		Count   int `gorm:"column:request_count"`
	}
	counts := make([]requestCount, 0)
	if err := DB.Table("quota_data").
		Select("token_id, SUM(count) AS request_count").
		Where("user_id = ? AND created_at >= ?", userId, since).
		Where("token_id > 0").
		Group("token_id").
		Find(&counts).Error; err != nil {
		return nil, err
	}

	countByToken := make(map[int]int, len(counts))
	for _, count := range counts {
		countByToken[count.TokenId] = count.Count
	}
	for i := range items {
		items[i].ActiveRequests = activeRequests[items[i].Id]
		items[i].Requests24h = countByToken[items[i].Id]
		if items[i].ActiveRequests > 0 {
			items[i].ResolvedActivity = "working"
		} else {
			items[i].ResolvedActivity = "idle"
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		iActive := items[i].ActiveRequests > 0
		jActive := items[j].ActiveRequests > 0
		if iActive != jActive {
			return iActive
		}
		if items[i].ActiveRequests != items[j].ActiveRequests {
			return items[i].ActiveRequests > items[j].ActiveRequests
		}
		if items[i].Requests24h != items[j].Requests24h {
			return items[i].Requests24h > items[j].Requests24h
		}
		if items[i].AccessedTime != items[j].AccessedTime {
			return items[i].AccessedTime > items[j].AccessedTime
		}
		if items[i].CreatedTime != items[j].CreatedTime {
			return items[i].CreatedTime > items[j].CreatedTime
		}
		return items[i].Id > items[j].Id
	})

	if len(items) > TokenActivityOfficeLimit {
		items = items[:TokenActivityOfficeLimit]
	}
	return items, nil
}
