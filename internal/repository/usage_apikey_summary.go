package repository

import (
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

// accumulateHourlyStat 将小时 stat 行按 api_group_key 累积，并按 model 精确计算 cost。
func (a *apiKeySummaryAccumulator) accumulateHourlyStat(row entities.UsageOverviewHourlyStat, pricingByModel map[string]entities.ModelPriceSetting) {
	key := strings.TrimSpace(row.APIGroupKey)
	if key == "" {
		return
	}
	cost, costAvailable := apiKeySummaryRowCost(row.Model, row.ModelAlias, row.InputTokens, row.OutputTokens, row.CachedTokens, row.CacheReadTokens, row.CacheCreationTokens, pricingByModel)
	item := a.items[key]
	if item == nil {
		item = &dto.UsageOverviewAPIKeySummary{APIGroupKey: key, CostAvailable: true}
		a.items[key] = item
	}
	item.RequestCount += row.RequestCount
	item.TotalTokens += row.TotalTokens
	item.InputTokens += row.InputTokens
	item.OutputTokens += row.OutputTokens
	item.CachedTokens += row.CachedTokens
	item.CostUSD += cost
	if !costAvailable {
		item.CostAvailable = false
	}
}

// accumulateDailyStat 将天 stat 行按 api_group_key 累积，并按 model 精确计算 cost。
func (a *apiKeySummaryAccumulator) accumulateDailyStat(row entities.UsageOverviewDailyStat, pricingByModel map[string]entities.ModelPriceSetting) {
	key := strings.TrimSpace(row.APIGroupKey)
	if key == "" {
		return
	}
	cost, costAvailable := apiKeySummaryRowCost(row.Model, row.ModelAlias, row.InputTokens, row.OutputTokens, row.CachedTokens, row.CacheReadTokens, row.CacheCreationTokens, pricingByModel)
	item := a.items[key]
	if item == nil {
		item = &dto.UsageOverviewAPIKeySummary{APIGroupKey: key, CostAvailable: true}
		a.items[key] = item
	}
	item.RequestCount += row.RequestCount
	item.TotalTokens += row.TotalTokens
	item.InputTokens += row.InputTokens
	item.OutputTokens += row.OutputTokens
	item.CachedTokens += row.CachedTokens
	item.CostUSD += cost
	if !costAvailable {
		item.CostAvailable = false
	}
}

// accumulateEvent 将边界原始事件按 api_group_key 累积。计价按真实 model 优先、缺价时回退 ModelAlias。
func (a *apiKeySummaryAccumulator) accumulateEvent(event entities.UsageEvent, pricingByModel map[string]entities.ModelPriceSetting) {
	key := strings.TrimSpace(event.APIGroupKey)
	if key == "" {
		return
	}
	eventAlias := ""
	if event.ModelAlias != nil {
		eventAlias = *event.ModelAlias
	}
	pricing, ok := matchPricingByMap(pricingByModel, event.Model, eventAlias)
	cost := helper.CalculateUsageEventCost(event, pricing)
	costAvailable := ok || !helper.UsageEventRequiresPricing(event)

	item := a.items[key]
	if item == nil {
		item = &dto.UsageOverviewAPIKeySummary{APIGroupKey: key, CostAvailable: true}
		a.items[key] = item
	}
	item.RequestCount++
	item.TotalTokens += event.TotalTokens
	item.InputTokens += event.InputTokens
	item.OutputTokens += event.OutputTokens
	item.CachedTokens += event.CachedTokens
	item.CostUSD += cost
	if !costAvailable {
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

// apiKeySummaryRowCost 按 model（缺价时回退 alias）计算单行 stat 的 cost。
func apiKeySummaryRowCost(model string, modelAlias string, inputTokens, outputTokens, cachedTokens, cacheReadTokens, cacheCreationTokens int64, pricingByModel map[string]entities.ModelPriceSetting) (float64, bool) {
	costInput := helper.UsageTokenCostInput{
		InputTokens:         inputTokens,
		OutputTokens:        outputTokens,
		CachedTokens:        cachedTokens,
		CacheReadTokens:     cacheReadTokens,
		CacheCreationTokens: cacheCreationTokens,
	}
	pricing, ok := matchPricingByMap(pricingByModel, model, modelAlias)
	if !ok {
		return 0, !helper.UsageTokenInputRequiresPricing(costInput)
	}
	return helper.CalculateUsageTokenCost(costInput, pricing), true
}

// accumulateAPIKeySummaryFromOverview 独立执行 API Key 汇总累积，复用主 Overview 的 filter 和时间窗口逻辑。
func accumulateAPIKeySummaryFromOverview(db *gorm.DB, overview *dto.UsageOverviewRecord, filter dto.UsageQueryFilter, pricingByModel map[string]entities.ModelPriceSetting, recentCache *UsageRecentEventCache, acc *apiKeySummaryAccumulator) error {
	queryNow := usageOverviewQueryNow(filter)
	effectiveFilter := usageOverviewEffectiveFilter(filter, queryNow)

	fullStart, fullEnd := usageOverviewFullHourWindow(*effectiveFilter.StartTime, *effectiveFilter.EndTime)
	currentRight := usageOverviewCurrentRightBoundary(filter, queryNow)
	rawEventWindows := usageOverviewRawEventWindows(effectiveFilter, overview.Health, fullStart, fullEnd, currentRight)

	// 边界原始事件
	boundaryEvents, err := loadUsageOverviewRawEventWindowsWithFilter(db, effectiveFilter, rawEventWindows, recentCache)
	if err != nil {
		return err
	}
	for _, event := range boundaryEvents {
		if usageOverviewEventInsideWindow(event, fullStart, fullEnd) {
			continue
		}
		acc.accumulateEvent(event, pricingByModel)
	}

	// 小时 / 天 stats
	if fullEnd.After(fullStart) {
		windowMinutes := computeWindowMinutes(effectiveFilter)
		bucketByDay := shouldBucketUsageOverviewByDay(effectiveFilter, windowMinutes)
		fullDayStart, fullDayEnd := usageOverviewFullDayWindow(fullStart, fullEnd)
		if !bucketByDay || !fullDayEnd.After(fullDayStart) {
			hourlyRows, err := loadUsageOverviewHourlyStatsWithFilter(db, effectiveFilter, fullStart, fullEnd)
			if err != nil {
				return err
			}
			for _, row := range hourlyRows {
				acc.accumulateHourlyStat(row, pricingByModel)
			}
		} else {
			dailyRows, err := loadUsageOverviewDailyStatsWithFilter(db, effectiveFilter, fullDayStart, fullDayEnd)
			if err != nil {
				return err
			}
			for _, row := range dailyRows {
				acc.accumulateDailyStat(row, pricingByModel)
			}
			for _, window := range []struct{ start, end time.Time }{{fullStart, fullDayStart}, {fullDayEnd, fullEnd}} {
				if !window.end.After(window.start) {
					continue
				}
				hourlyRows, err := loadUsageOverviewHourlyStatsWithFilter(db, effectiveFilter, window.start, window.end)
				if err != nil {
					return err
				}
				for _, row := range hourlyRows {
					acc.accumulateHourlyStat(row, pricingByModel)
				}
			}
		}
	}
	return nil
}
