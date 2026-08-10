package entities

import (
	"reflect"
	"testing"
)

func TestAllIncludesCoreModels(t *testing.T) {
	items := All()
	expected := []any{
		&UsageEvent{},
		&UsageEventArchive{},
		&RedisUsageInbox{},
		&ModelPriceSetting{},
		&ModelPriceRule{},
		&UsageIdentity{},
		&CPAAPIKey{},
		&UsageOverviewHourlyStat{},
		&UsageOverviewDailyStat{},
		// v1.14 起三类聚合用统一 checkpoint 表。
		&UsageAggregationCheckpoint{},
		// 本地排行统计（v1.14.2 #392）。
		&LocalRankingPeriodStat{},
		&UsageActivityStat{},
		// Latency hour/day 共用一张可合并聚合表。
		&UsageLatencyStat{},
		&AuthSession{},
		&AppSetting{},
	}
	if len(items) != len(expected) {
		t.Fatalf("expected %d registered models, got %d", len(expected), len(items))
	}
	for index := range expected {
		if got, want := reflect.TypeOf(items[index]), reflect.TypeOf(expected[index]); got != want {
			t.Fatalf("expected model %d to be %v, got %v", index, want, got)
		}
	}
}
