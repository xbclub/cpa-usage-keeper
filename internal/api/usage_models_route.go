package api

import (
	"net/http"
	"time"

	"cpa-usage-keeper/internal/service"
	"cpa-usage-keeper/internal/timeutil"
	"github.com/gin-gonic/gin"
)

// registerUsageModelsRoute 是 fork-unique 的 /usage/models 端点。
// 返回 DISTINCT model 列表，不受 model filter 影响。
func registerUsageModelsRoute(router gin.IRoutes, usageProvider service.UsageProvider) {
	router.GET("/usage/models", func(c *gin.Context) {
		if usageProvider == nil {
			c.JSON(http.StatusOK, gin.H{"models": []string{}})
			return
		}
		filter, err := parseUsageTimeFilterQuery(c.Request, timeutil.NormalizeStorageTime(time.Now()))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		models, err := usageProvider.ListOverviewModels(c.Request.Context(), filter)
		if err != nil {
			writeInternalError(c, "get overview models failed", err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"models": models})
	})
}
