package controller

import (
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GetAllRedemptions(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	redemptions, total, err := model.GetAllRedemptions(pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(redemptions)
	common.ApiSuccess(c, pageInfo)
	return
}

func SearchRedemptions(c *gin.Context) {
	keyword := c.Query("keyword")
	status := c.Query("status")
	pageInfo := common.GetPageQuery(c)
	redemptions, total, err := model.SearchRedemptions(keyword, status, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(redemptions)
	common.ApiSuccess(c, pageInfo)
	return
}

func GetRedemption(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	redemption, err := model.GetRedemptionById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    redemption,
	})
	return
}

func AddRedemption(c *gin.Context) {
	if !operation_setting.IsPaymentComplianceConfirmed() {
		common.ApiErrorT(c, "Payment, redemption, subscription, and invitation reward features are disabled. The administrator must confirm compliance terms before enabling them.")
		return
	}

	var request struct {
		Name             string `json:"name"`
		Quota            int    `json:"quota"`
		Count            int    `json:"count"`
		ExpiredTime      int64  `json:"expired_time"`
		DistributionType string `json:"distribution_type"`
		PriceCents       int64  `json:"price_cents"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	if utf8.RuneCountInString(request.Name) == 0 || utf8.RuneCountInString(request.Name) > 20 {
		common.ApiErrorT(c, "Redemption code name length must be between 1-20")
		return
	}
	if request.Count <= 0 {
		common.ApiErrorT(c, "Redemption code count must be greater than 0")
		return
	}
	if request.Count > 100 {
		common.ApiErrorT(c, "Maximum 100 redemption codes can be generated at once")
		return
	}
	if request.Quota <= 0 {
		common.ApiErrorT(c, "Redemption quota must be positive")
		return
	}
	if err := common.ValidateWalletQuota(request.Quota); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.ValidateRedemptionDistribution(request.DistributionType, request.PriceCents); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := validateExpiredTime(request.ExpiredTime); err != nil {
		common.ApiError(c, err)
		return
	}
	var keys []string
	// 批次必须原子提交，不能返回部分密钥后留下不完整的销售库存。
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		for range request.Count {
			key := common.GetUUID()
			cleanRedemption := model.Redemption{
				UserId:           c.GetInt("id"),
				Name:             request.Name,
				Key:              key,
				CreatedTime:      common.GetTimestamp(),
				Quota:            request.Quota,
				ExpiredTime:      request.ExpiredTime,
				DistributionType: request.DistributionType,
				PriceCents:       request.PriceCents,
			}
			if err := model.InsertRedemptionTx(tx, &cleanRedemption); err != nil {
				return err
			}
			keys = append(keys, key)
		}
		return nil
	})
	if err != nil {
		common.SysError(common.LogText("failed to insert redemption batch: %s", err.Error()))
		common.ApiError(c, common.NewMessage("Failed to create redemption codes, please try again later"))
		return
	}
	recordManageAudit(c, "redemption.create", map[string]any{
		"name":              request.Name,
		"count":             request.Count,
		"quota":             logger.FormatQuota(request.Quota),
		"distribution_type": request.DistributionType,
		"price_cents":       request.PriceCents,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    keys,
	})
	return
}

func DeleteRedemption(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	err := model.DeleteRedemptionById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func UpdateRedemption(c *gin.Context) {
	statusOnly := c.Query("status_only") != ""
	var request struct {
		Id               int    `json:"id"`
		Name             string `json:"name"`
		Quota            int    `json:"quota"`
		ExpiredTime      int64  `json:"expired_time"`
		Status           int    `json:"status"`
		DistributionType string `json:"distribution_type"`
		PriceCents       int64  `json:"price_cents"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	cleanRedemption, err := model.GetRedemptionById(request.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if cleanRedemption.Status == common.RedemptionCodeStatusUsed || cleanRedemption.RedeemedTime != 0 {
		common.ApiError(c, common.NewMessage("Redeemed redemption codes cannot be edited"))
		return
	}
	if statusOnly {
		cleanRedemption.Status = request.Status
		err = cleanRedemption.UpdateStatus()
	} else {
		if request.Quota <= 0 {
			common.ApiErrorT(c, "Redemption quota must be positive")
			return
		}
		if err := common.ValidateWalletQuota(request.Quota); err != nil {
			common.ApiError(c, err)
			return
		}
		if err := model.ValidateRedemptionDistribution(request.DistributionType, request.PriceCents); err != nil {
			common.ApiError(c, err)
			return
		}
		if err := validateExpiredTime(request.ExpiredTime); err != nil {
			common.ApiError(c, err)
			return
		}
		cleanRedemption.Name = request.Name
		cleanRedemption.Quota = request.Quota
		cleanRedemption.ExpiredTime = request.ExpiredTime
		cleanRedemption.DistributionType = request.DistributionType
		cleanRedemption.PriceCents = request.PriceCents
		err = cleanRedemption.Update()
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    cleanRedemption,
	})
	return
}

func DeleteInvalidRedemption(c *gin.Context) {
	rows, err := model.DeleteInvalidRedemptions()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    rows,
	})
	return
}

func validateExpiredTime(expired int64) error {
	if expired != 0 && expired < common.GetTimestamp() {
		return common.NewMessage("Expiration time cannot be earlier than current time")
	}
	return nil
}

func DeleteRedemptionBatch(c *gin.Context) {
	var request struct {
		Ids []int `json:"ids" binding:"required,min=1,max=1000,dive,gt=0"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	count, err := model.BatchDeleteRedemptions(request.Ids)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "redemption.delete_batch", map[string]any{
		"count":                    count,
		"total":                    len(request.Ids),
		"requested_redemption_ids": request.Ids,
	})
	common.ApiSuccess(c, count)
}
