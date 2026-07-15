package controller

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

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
