package repository

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository/dto"
)

// TestRecentCacheFiltersByModelOnBoundedEvents 验证 recent-cache 的 bounded Events 路径按 filter.Models 精确过滤。
// 直接测试 loadUsageOverviewRawEventWindowsWithFilter，确保 cache 路径的 model 过滤生效。
func TestRecentCacheFiltersByModelOnBoundedEvents(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	cache := newEmptyUsageRecentEventCache(UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	t.Cleanup(cache.Close)
	cache.appendEvents([]entities.UsageEvent{
		{EventKey: "model-a-bounded", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now.Add(-10 * time.Minute), InputTokens: 100, TotalTokens: 100},
		{EventKey: "model-b-bounded", APIGroupKey: "provider-a", Model: "model-b", Timestamp: now.Add(-9 * time.Minute), InputTokens: 200, TotalTokens: 200},
	})

	// bounded Events 窗口：start 到 end，由 cache 承接。
	windowStart := now.Add(-15 * time.Minute)
	windowEnd := now.Add(-5 * time.Minute)
	windows := []usageOverviewRawEventWindow{{start: windowStart, end: windowEnd, includeEnd: true}}
	events, err := loadUsageOverviewRawEventWindowsWithFilter(db, dto.UsageQueryFilter{
		APIGroupKey: "provider-a",
		Models:      []string{"model-a"},
	}, windows, cache)
	if err != nil {
		t.Fatalf("loadUsageOverviewRawEventWindowsWithFilter returned error: %v", err)
	}
	if len(events) != 1 || events[0].Model != "model-a" || events[0].InputTokens != 100 {
		t.Fatalf("expected only model-a event from bounded Events cache path, got %+v", events)
	}
}

// TestRecentCacheFiltersByModelOnEventsSince 验证 recent-cache 的 EventsSince（current-right）路径按 filter.Models 精确过滤。
func TestRecentCacheFiltersByModelOnEventsSince(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	cache := newEmptyUsageRecentEventCache(UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	t.Cleanup(cache.Close)
	cache.appendEvents([]entities.UsageEvent{
		{EventKey: "model-a-current", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now.Add(-3 * time.Minute), InputTokens: 50, TotalTokens: 50},
		{EventKey: "model-b-current", APIGroupKey: "provider-a", Model: "model-b", Timestamp: now.Add(-2 * time.Minute), InputTokens: 70, TotalTokens: 70},
	})

	// current-right 窗口：start 到 now，走 EventsSince 路径。
	windowStart := now.Add(-10 * time.Minute)
	windows := []usageOverviewRawEventWindow{{start: windowStart, end: now, includeEnd: true, currentRight: true}}
	events, err := loadUsageOverviewRawEventWindowsWithFilter(db, dto.UsageQueryFilter{
		APIGroupKey: "provider-a",
		Models:      []string{"model-a"},
	}, windows, cache)
	if err != nil {
		t.Fatalf("loadUsageOverviewRawEventWindowsWithFilter returned error: %v", err)
	}
	if len(events) != 1 || events[0].Model != "model-a" || events[0].InputTokens != 50 {
		t.Fatalf("expected only model-a event from EventsSince cache path, got %+v", events)
	}
}

// TestRecentCacheEmptyModelsDoesNotFilter 验证空 Models 不过滤模型（语义一致性）。
func TestRecentCacheEmptyModelsDoesNotFilter(t *testing.T) {
	db := openTestDatabase(t)
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)

	cache := newEmptyUsageRecentEventCache(UsageRecentEventCacheOptions{Now: func() time.Time { return now }})
	t.Cleanup(cache.Close)
	cache.appendEvents([]entities.UsageEvent{
		{EventKey: "model-a-empty", APIGroupKey: "provider-a", Model: "model-a", Timestamp: now.Add(-10 * time.Minute), InputTokens: 100, TotalTokens: 100},
		{EventKey: "model-b-empty", APIGroupKey: "provider-a", Model: "model-b", Timestamp: now.Add(-9 * time.Minute), InputTokens: 200, TotalTokens: 200},
	})

	windowStart := now.Add(-15 * time.Minute)
	windowEnd := now.Add(-5 * time.Minute)
	windows := []usageOverviewRawEventWindow{{start: windowStart, end: windowEnd, includeEnd: true}}
	events, err := loadUsageOverviewRawEventWindowsWithFilter(db, dto.UsageQueryFilter{
		APIGroupKey: "provider-a",
	}, windows, cache)
	if err != nil {
		t.Fatalf("loadUsageOverviewRawEventWindowsWithFilter returned error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected both models when Models is empty, got %d events: %+v", len(events), events)
	}
}

// TestHealthTotalsFiltersByModelOnHourlyStats 验证 loadUsageOverviewHealthTotalsWithFilter 的 totalsQuery
// 在 UsageOverviewHourlyStat 上应用 model filter。model-a 有成功事件，model-b 有失败事件，
// 查询只选 model-a 时，health totals 不得包含 model-b 的失败数据。
func TestHealthTotalsFiltersByModelOnHourlyStats(t *testing.T) {
	db := openTestDatabase(t)
	hourStart := time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC)

	if _, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{Model: "model-a"}); err != nil {
		t.Fatalf("UpsertModelPriceSetting model-a: %v", err)
	}

	// 插入 2 个模型的事件，触发 hourly stats 聚合。
	if _, _, err := InsertUsageEvents(db, []entities.UsageEvent{
		{EventKey: "model-a-success", APIGroupKey: "provider-a", Model: "model-a", Timestamp: hourStart.Add(5 * time.Minute), TotalTokens: 100},
		{EventKey: "model-b-failed", APIGroupKey: "provider-a", Model: "model-b", Timestamp: hourStart.Add(6 * time.Minute), Failed: true, TotalTokens: 50},
	}); err != nil {
		t.Fatalf("InsertUsageEvents: %v", err)
	}

	// 强制触发聚合，让 hourly stats 表有数据。
	if err := AggregateUsageOverviewStats(context.Background(), db, hourStart.Add(time.Hour)); err != nil {
		t.Fatalf("AggregateUsageOverviewStats: %v", err)
	}

	start := hourStart
	end := hourStart.Add(time.Hour)
	queryNow := end
	overview, err := BuildUsageOverviewWithFilter(db, dto.UsageQueryFilter{
		Range:     "custom",
		StartTime: &start,
		EndTime:   &end,
		Models:    []string{"model-a"},
		QueryNow:  &queryNow,
	})
	if err != nil {
		t.Fatalf("BuildUsageOverviewWithFilter returned error: %v", err)
	}

	// health totals 只应统计 model-a（1 个成功），不含 model-b 的失败。
	if overview.Health.TotalFailure != 0 {
		t.Fatalf("expected health totals to exclude model-b failures, got TotalFailure=%d", overview.Health.TotalFailure)
	}
	if overview.Health.TotalSuccess != 1 {
		t.Fatalf("expected health totals to count only model-a success, got TotalSuccess=%d", overview.Health.TotalSuccess)
	}
}

// TestAPIKeySummaryUsesModelAliasForPricing 验证 APIKeySummary 的计价复用 matchPricingByMap，
// 真实 model 有价格时优先用 model 价格；model 无价格时回退 alias；都无价格且存在计费 token 时 CostAvailable=false。
func TestAPIKeySummaryUsesModelAliasForPricing(t *testing.T) {
	cases := []struct {
		name            string
		model           string
		alias           string
		pricedModel     string
		expectCost      float64
		expectAvailable bool
	}{
		{
			name:            "both priced uses model price",
			model:           "base-model",
			alias:           "alias-model",
			pricedModel:     "base-model",
			expectCost:      0.3,
			expectAvailable: true,
		},
		{
			name:            "model missing falls back to alias",
			model:           "missing-model",
			alias:           "alias-model",
			pricedModel:     "alias-model",
			expectCost:      0.3,
			expectAvailable: true,
		},
		{
			name:            "both missing with billable tokens unavailable",
			model:           "missing-model",
			alias:           "also-missing",
			pricedModel:     "",
			expectCost:      0,
			expectAvailable: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runAPIKeySummaryPricingCase(t, tc.model, tc.alias, tc.pricedModel, tc.expectCost, tc.expectAvailable)
		})
	}
}

func runAPIKeySummaryPricingCase(t *testing.T, eventModel string, eventAlias string, pricedModel string, expectCost float64, expectAvailable bool) {
	t.Helper()
	db := openTestDatabase(t)
	pricingByModel := map[string]entities.ModelPriceSetting{}
	if pricedModel != "" {
		setting, err := UpsertModelPriceSetting(db, dto.ModelPriceSettingInput{
			Model:             pricedModel,
			PromptPricePer1M:  3,
			CompletionPricePer1M: 0,
			CacheReadPricePer1M: 0,
		})
		if err != nil {
			t.Fatalf("UpsertModelPriceSetting %s: %v", pricedModel, err)
		}
		pricingByModel[pricedModel] = *setting
	}

	acc := newAPIKeySummaryAccumulator()
	alias := eventAlias
	acc.accumulateEvent(entities.UsageEvent{
		APIGroupKey:  "provider-a",
		Model:        eventModel,
		ModelAlias:   &alias,
		InputTokens:  100000,
		OutputTokens: 0,
		TotalTokens:  100000,
	}, pricingByModel)

	slice := acc.toSlice()
	if len(slice) != 1 {
		t.Fatalf("expected 1 APIKeySummary item, got %d", len(slice))
	}
	item := slice[0]
	if item.CostAvailable != expectAvailable {
		t.Fatalf("expected CostAvailable=%v, got %v (cost=%.6f)", expectAvailable, item.CostAvailable, item.CostUSD)
	}
	if expectAvailable && item.CostUSD < expectCost-0.000001 || item.CostUSD > expectCost+0.000001 {
		t.Fatalf("expected cost=%.6f, got %.6f", expectCost, item.CostUSD)
	}
}
