package repository

import (
	"fmt"
	"strings"

	"cpa-usage-keeper/internal/repository/dto"
	"gorm.io/gorm"
)

// ListOverviewModelNamesWithFilter 是 fork-unique 的 /usage/models endpoint 仓储层实现。
// 返回 DISTINCT model 列表，不受 model filter 影响（避免筛选时下拉框缩小）。
func ListOverviewModelNamesWithFilter(db *gorm.DB, filter dto.UsageQueryFilter) ([]string, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	query := applyUsageQueryWindow(queryUsageEvents(db), filter)
	if apiGroupKey := strings.TrimSpace(filter.APIGroupKey); apiGroupKey != "" {
		query = query.Where("api_group_key = ?", apiGroupKey)
	}
	var values []string
	if err := query.Select("DISTINCT model").Where("model <> ''").Order("model ASC").Pluck("model", &values).Error; err != nil {
		return nil, fmt.Errorf("load overview model names: %w", err)
	}
	return values, nil
}

// usage_apikey_summary.go 原本是 fork-unique 的 API Key 汇总功能。
// 上游 v1.13.6 重构了 stat projection 系统（usageOverviewStatProjection），
// APIKeySummary accumulator 待后续适配新架构后恢复。
