package repository

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/helper"
	"cpa-usage-keeper/internal/repository/dto"
	"gorm.io/gorm"
)

// apiKeySummaryAccumulator 在概览聚合过程中累积每个 API Key 的用量汇总。
type apiKeySummaryAccumulator struct {
	items map[string]*dto.UsageOverviewAPIKeySummary
}

func newAPIKeySummaryAccumulator() apiKeySummaryAccumulator {
	return apiKeySummaryAccumulator{items: make(map[string]*dto.UsageOverviewAPIKeySummary)}
}

// accumulateHourlyStat 将小时 stat 行按 api_group_key 累积。
func (a *apiKeySummaryAccumulator) accumulateHourlyStat(row entities.UsageOverviewHourlyStat, costResolver *UsageCostResolver) {
	key := strings.TrimSpace(row.APIGroupKey)
	if key == "" {
		return
	}
	costResult := costResolver.Calculate(UsageCostSubject{
		Model:      row.Model,
		ModelAlias: row.ModelAlias,
		Tokens: helper.UsageTokenCostInput{
			InputTokens:         row.InputTokens,
			CacheReadTokens:     row.CacheReadTokens,
			CacheCreationTokens: row.CacheCreationTokens,
		},
	})
	item := a.items[key]
	if item == nil {
		item = &dto.UsageOverviewAPIKeySummary{APIGroupKey: key, CostAvailable: true}
		a.items[key] = item
	}
	item.RequestCount += row.RequestCount
	item.TotalTokens += row.TotalTokens
	item.InputTokens += row.InputTokens
	item.CacheReadTokens += row.CacheReadTokens
	item.CacheCreationTokens += row.CacheCreationTokens
	item.CostUSD += costResult.Cost.TotalCostUSD
	if !costResult.Available {
		item.CostAvailable = false
	}
}

// accumulateDailyStat 将天 stat 行按 api_group_key 累积。
func (a *apiKeySummaryAccumulator) accumulateDailyStat(row entities.UsageOverviewDailyStat, costResolver *UsageCostResolver) {
	key := strings.TrimSpace(row.APIGroupKey)
	if key == "" {
		return
	}
	costResult := costResolver.Calculate(UsageCostSubject{
		Model:      row.Model,
		ModelAlias: row.ModelAlias,
		Tokens: helper.UsageTokenCostInput{
			InputTokens:         row.InputTokens,
			CacheReadTokens:     row.CacheReadTokens,
			CacheCreationTokens: row.CacheCreationTokens,
		},
	})
	item := a.items[key]
	if item == nil {
		item = &dto.UsageOverviewAPIKeySummary{APIGroupKey: key, CostAvailable: true}
		a.items[key] = item
	}
	item.RequestCount += row.RequestCount
	item.TotalTokens += row.TotalTokens
	item.InputTokens += row.InputTokens
	item.CacheReadTokens += row.CacheReadTokens
	item.CacheCreationTokens += row.CacheCreationTokens
	item.CostUSD += costResult.Cost.TotalCostUSD
	if !costResult.Available {
		item.CostAvailable = false
	}
}

// accumulateEvent 将边界原始事件按 api_group_key 累积。
func (a *apiKeySummaryAccumulator) accumulateEvent(event entities.UsageEvent, costResolver *UsageCostResolver) {
	key := strings.TrimSpace(event.APIGroupKey)
	if key == "" {
		return
	}
	costResult := costResolver.CalculateEvent(event)
	item := a.items[key]
	if item == nil {
		item = &dto.UsageOverviewAPIKeySummary{APIGroupKey: key, CostAvailable: true}
		a.items[key] = item
	}
	item.RequestCount++
	item.TotalTokens += event.TotalTokens
	item.InputTokens += event.InputTokens
	item.CacheReadTokens += event.CacheReadTokens
	item.CacheCreationTokens += event.CacheCreationTokens
	item.CostUSD += costResult.Cost.TotalCostUSD
	if !costResult.Available {
		item.CostAvailable = false
	}
}

// toSlice 返回按请求数降序排列的汇总列表。
func (a *apiKeySummaryAccumulator) toSlice() []dto.UsageOverviewAPIKeySummary {
	result := make([]dto.UsageOverviewAPIKeySummary, 0, len(a.items))
	for _, item := range a.items {
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].RequestCount == result[j].RequestCount {
			return result[i].APIGroupKey < result[j].APIGroupKey
		}
		return result[i].RequestCount > result[j].RequestCount
	})
	return result
}

// accumulateAPIKeySummaryFromOverview 独立执行 API Key 汇总累积。
// 使用 entity 级别的 stat 行（含 api_group_key），不使用 usageOverviewStatProjection（按 bucket_start 分组，不含 api_group_key）。
func accumulateAPIKeySummaryFromOverview(db *gorm.DB, filter dto.UsageQueryFilter, costResolver *UsageCostResolver, recentCache *UsageRecentEventCache, acc *apiKeySummaryAccumulator) error {
	queryNow := usageOverviewQueryNow(filter)
	effectiveFilter := usageOverviewEffectiveFilter(filter, queryNow)

	fullStart, fullEnd := usageOverviewFullHourWindow(*effectiveFilter.StartTime, *effectiveFilter.EndTime)
	currentRight := usageOverviewCurrentRightBoundary(filter, queryNow)
	rawEventWindows := usageOverviewRawEventWindows(effectiveFilter, fullStart, fullEnd, currentRight)

	// 边界原始事件
	boundaryEvents, err := loadUsageOverviewRawEventWindowsWithFilter(db, effectiveFilter, rawEventWindows, recentCache)
	if err != nil {
		return err
	}
	for _, event := range boundaryEvents {
		if usageOverviewEventInsideWindow(event, fullStart, fullEnd) {
			continue
		}
		acc.accumulateEvent(event, costResolver)
	}

	// 小时 / 天 stats（使用 entity 级别，含 api_group_key）
	if fullEnd.After(fullStart) {
		windowMinutes := computeWindowMinutes(effectiveFilter)
		bucketByDay := shouldBucketUsageOverviewByDay(effectiveFilter, windowMinutes)
		fullDayStart, fullDayEnd := usageOverviewFullDayWindow(fullStart, fullEnd)
		if !bucketByDay || !fullDayEnd.After(fullDayStart) {
			rows, err := loadUsageOverviewHourlyStats(db, effectiveFilter, fullStart, fullEnd, false)
			if err != nil {
				return err
			}
			for _, row := range rows {
				acc.accumulateHourlyStat(row, costResolver)
			}
		} else {
			rows, err := loadUsageOverviewDailyStats(db, effectiveFilter, fullDayStart, fullDayEnd, false)
			if err != nil {
				return err
			}
			for _, row := range rows {
				acc.accumulateDailyStat(row, costResolver)
			}
			for _, window := range []struct{ start, end time.Time }{{fullStart, fullDayStart}, {fullDayEnd, fullEnd}} {
				if !window.end.After(window.start) {
					continue
				}
				hourlyRows, err := loadUsageOverviewHourlyStats(db, effectiveFilter, window.start, window.end, false)
				if err != nil {
					return err
				}
				for _, row := range hourlyRows {
					acc.accumulateHourlyStat(row, costResolver)
				}
			}
		}
	}
	return nil
}

// ListOverviewModelNamesWithFilter 是 fork-unique 的 /usage/models endpoint 仓储层实现。
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
