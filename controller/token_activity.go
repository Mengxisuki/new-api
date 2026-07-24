package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const maxTokenUsageRangeSeconds = 31 * 24 * 60 * 60

func GetUserTokenActivity(c *gin.Context) {
	userId := c.GetInt("id")
	items, err := model.GetUserTokenActivity(
		userId,
		service.GetActiveTokenCounts(userId),
		time.Now().Add(-24*time.Hour).Unix(),
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"server_time": common.GetTimestamp(),
		"tokens":      items,
	})
}

func GetUserTokenUsage(c *gin.Context) {
	startTimestamp, err := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	if err != nil || startTimestamp <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid start_timestamp"})
		return
	}
	endTimestamp, err := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	if err != nil || endTimestamp <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid end_timestamp"})
		return
	}
	if endTimestamp < startTimestamp || endTimestamp-startTimestamp > maxTokenUsageRangeSeconds {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid time range"})
		return
	}

	keys, points, err := model.GetUserTokenUsage(c.GetInt("id"), startTimestamp, endTimestamp)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"keys":   keys,
		"points": points,
	})
}
