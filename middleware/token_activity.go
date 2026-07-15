package middleware

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func TokenActivity() gin.HandlerFunc {
	return func(c *gin.Context) {
		isRealtime := c.Request.Method == http.MethodGet && strings.HasSuffix(c.Request.URL.Path, "/realtime")
		if c.Request.Method != http.MethodPost && !isRealtime {
			c.Next()
			return
		}

		finish := service.BeginTokenActivity(
			c.GetInt("id"),
			c.GetInt("token_id"),
			c.GetString(common.RequestIdKey),
		)
		defer finish()
		c.Next()
	}
}
