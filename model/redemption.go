package model

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"gorm.io/gorm"
)

const (
	// 兑换码的售赠分类必须显式保存；空值仅代表历史记录无法追溯。
	RedemptionDistributionSale    = "sale"
	RedemptionDistributionGift    = "gift"
	RedemptionDistributionUnknown = "unknown"
)

type Redemption struct {
	Id               int            `json:"id"`
	UserId           int            `json:"user_id"`
	Key              string         `json:"key" gorm:"type:char(32);uniqueIndex"`
	Status           int            `json:"status" gorm:"default:1"`
	Name             string         `json:"name" gorm:"index"`
	Quota            int            `json:"quota" gorm:"default:100"`
	DistributionType string         `json:"distribution_type" gorm:"size:16;index;default:unknown"` // sale、gift 或历史 unknown。
	PriceCents       int64          `json:"price_cents" gorm:"type:bigint;not null;default:0"` // 售出码的单码人民币售价。
	CreatedTime      int64          `json:"created_time" gorm:"bigint"`
	RedeemedTime     int64          `json:"redeemed_time" gorm:"bigint"`
	Count            int            `json:"count" gorm:"-:all"` // only for api request
	UsedUserId       int            `json:"used_user_id"`
	DeletedAt        gorm.DeletedAt `gorm:"index"`
	ExpiredTime      int64          `json:"expired_time" gorm:"bigint"` // 过期时间，0 表示不过期
}

// ValidateRedemptionDistribution 校验人民币售价与售赠分类的组合，禁止把额度面额当成售价。
func ValidateRedemptionDistribution(distributionType string, priceCents int64) error {
	if distributionType == RedemptionDistributionGift {
		if priceCents != 0 {
			return errors.New("gift redemption price must be zero")
		}
		return nil
	}
	if distributionType == RedemptionDistributionSale {
		if priceCents <= 0 || priceCents > MaxCreditCents {
			return errors.New("sale redemption price is invalid")
		}
		return nil
	}
	return errors.New("redemption distribution type is required")
}

func normalizeRedemptionDistribution(distributionType string) string {
	if distributionType == "" {
		return RedemptionDistributionUnknown
	}
	return distributionType
}

// AfterFind 让旧记录以“未分类”返回，但不伪造历史售价。
func (redemption *Redemption) AfterFind(_ *gorm.DB) error {
	redemption.DistributionType = normalizeRedemptionDistribution(redemption.DistributionType)
	return nil
}

func GetAllRedemptions(startIdx int, num int) (redemptions []*Redemption, total int64, err error) {
	// 开始事务
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 获取总数
	err = tx.Model(&Redemption{}).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// 获取分页数据
	err = tx.Order("id desc").Limit(num).Offset(startIdx).Find(&redemptions).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// 提交事务
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return redemptions, total, nil
}

func SearchRedemptions(keyword string, status string, startIdx int, num int) (redemptions []*Redemption, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&Redemption{})

	if keyword != "" {
		if id, err := strconv.Atoi(keyword); err == nil {
			query = query.Where("id = ? OR name LIKE ?", id, keyword+"%")
		} else {
			query = query.Where("name LIKE ?", keyword+"%")
		}
	}

	if status != "" {
		now := common.GetTimestamp()
		switch status {
		case "expired":
			query = query.Where(
				"status = ? AND expired_time != 0 AND expired_time < ?",
				common.RedemptionCodeStatusEnabled,
				now,
			)
		case strconv.Itoa(common.RedemptionCodeStatusEnabled):
			query = query.Where(
				"status = ? AND (expired_time = 0 OR expired_time >= ?)",
				common.RedemptionCodeStatusEnabled,
				now,
			)
		case strconv.Itoa(common.RedemptionCodeStatusDisabled):
			query = query.Where("status = ?", common.RedemptionCodeStatusDisabled)
		case strconv.Itoa(common.RedemptionCodeStatusUsed):
			query = query.Where("status = ?", common.RedemptionCodeStatusUsed)
		}
	}

	// Get total count
	err = query.Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated data
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&redemptions).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return redemptions, total, nil
}

func GetRedemptionById(id int) (*Redemption, error) {
	if id == 0 {
		return nil, common.NewMessage("ID is empty")
	}
	redemption := Redemption{Id: id}
	var err error = nil
	err = DB.First(&redemption, "id = ?", id).Error
	return &redemption, err
}

func Redeem(key string, userId int) (quota int, err error) {
	if key == "" {
		return 0, errors.New("redemption code not provided")
	}
	if userId == 0 {
		return 0, errors.New("invalid user id")
	}
	redemption := &Redemption{}

	keyCol := "`key`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		keyCol = `"key"`
	}
	common.RandomSleep()
	err = DB.Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where(keyCol+" = ?", key).First(redemption).Error
		if err != nil {
			return errors.New("invalid redemption code")
		}
		if redemption.Status != common.RedemptionCodeStatusEnabled {
			return errors.New("redemption code already used")
		}
		if redemption.ExpiredTime != 0 && redemption.ExpiredTime < common.GetTimestamp() {
			return errors.New("redemption code expired")
		}
		// Compare-and-swap on status: only the transaction that flips
		// enabled -> used may credit quota, so a concurrent redeem of the
		// same code loses here even without a row lock (e.g. on SQLite).
		result := tx.Model(&Redemption{}).
			Where("id = ? AND status = ?", redemption.Id, common.RedemptionCodeStatusEnabled).
			Updates(map[string]any{
				"redeemed_time": common.GetTimestamp(),
				"status":        common.RedemptionCodeStatusUsed,
				"used_user_id":  userId,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("redemption code already used")
		}
		return creditTopUpQuota(tx, userId, redemption.Quota, nil)
	})
	if err != nil {
		common.SysError(common.LogText("redemption failed: %s", err.Error()))
		return 0, ErrRedeemFailed
	}
	syncCreditUserQuotaCache(userId, redemption.Quota, common.LogText("redemption"))
	RecordLog(userId, LogTypeTopup, common.NewMessage("Topped up {{quota}} with a redemption code, code ID {{id}}", map[string]any{"quota": logger.FormatQuota(redemption.Quota), "id": redemption.Id}))
	return redemption.Quota, nil
}

func (redemption *Redemption) Insert() error {
	return InsertRedemptionTx(DB, redemption)
}

// InsertRedemptionTx 复用创建校验，允许批次创建在同一个数据库事务中提交。
func InsertRedemptionTx(tx *gorm.DB, redemption *Redemption) error {
	if redemption.Quota <= 0 {
		return errors.New("redemption quota must be positive")
	}
	if err := common.ValidateWalletQuota(redemption.Quota); err != nil {
		return err
	}
	if err := ValidateRedemptionDistribution(redemption.DistributionType, redemption.PriceCents); err != nil {
		return err
	}
	return tx.Create(redemption).Error
}

// Update 只修改未兑换码的业务字段，避免旧页面覆盖已确认的收入事实。
// SelectUpdate 保留旧调用入口，但同样禁止把兑换码伪造为已兑换。
func (redemption *Redemption) SelectUpdate() error {
	return redemption.UpdateStatus()
}

func (redemption *Redemption) Update() error {
	if redemption.Status == common.RedemptionCodeStatusUsed {
		return errors.New("used redemption code cannot be updated")
	}
	if redemption.Quota <= 0 {
		return errors.New("redemption quota must be positive")
	}
	if err := common.ValidateWalletQuota(redemption.Quota); err != nil {
		return err
	}
	if err := ValidateRedemptionDistribution(redemption.DistributionType, redemption.PriceCents); err != nil {
		return err
	}
	result := DB.Model(&Redemption{}).
		Where("id = ? AND status <> ? AND redeemed_time = 0", redemption.Id, common.RedemptionCodeStatusUsed).
		Updates(map[string]any{
			"name":              redemption.Name,
			"quota":             redemption.Quota,
			"expired_time":      redemption.ExpiredTime,
			"distribution_type": redemption.DistributionType,
			"price_cents":       redemption.PriceCents,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("redemption code was redeemed or not found")
	}
	return nil
}

// UpdateStatus 仅允许在可用和停用之间切换，used 只能由 Redeem 事务产生。
func (redemption *Redemption) UpdateStatus() error {
	if redemption.Status != common.RedemptionCodeStatusEnabled && redemption.Status != common.RedemptionCodeStatusDisabled {
		return errors.New("invalid redemption status")
	}
	result := DB.Model(&Redemption{}).
		Where("id = ? AND status <> ? AND redeemed_time = 0", redemption.Id, common.RedemptionCodeStatusUsed).
		Update("status", redemption.Status)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("redemption code was redeemed or not found")
	}
	return nil
}

func (redemption *Redemption) Delete() error {
	var err error
	err = DB.Delete(redemption).Error
	return err
}

func DeleteRedemptionById(id int) (err error) {
	if id == 0 {
		return common.NewMessage("ID is empty")
	}
	redemption := Redemption{Id: id}
	err = DB.Where(redemption).First(&redemption).Error
	if err != nil {
		return err
	}
	return redemption.Delete()
}

func DeleteInvalidRedemptions() (int64, error) {
	now := common.GetTimestamp()
	result := DB.Where("status IN ? OR (status = ? AND expired_time != 0 AND expired_time < ?)", []int{common.RedemptionCodeStatusUsed, common.RedemptionCodeStatusDisabled}, common.RedemptionCodeStatusEnabled, now).Delete(&Redemption{})
	return result.RowsAffected, result.Error
}

// BatchDeleteRedemptions soft-deletes the selected codes in one statement.
func BatchDeleteRedemptions(ids []int) (int64, error) {
	if len(ids) == 0 || len(ids) > 1000 {
		return 0, errors.New("select between 1 and 1000 redemption codes")
	}
	for _, id := range ids {
		if id <= 0 {
			return 0, errors.New("redemption IDs must be positive")
		}
	}
	result := DB.Where("id IN ?", ids).Delete(&Redemption{})
	return result.RowsAffected, result.Error
}
