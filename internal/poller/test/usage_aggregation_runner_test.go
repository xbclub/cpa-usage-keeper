package poller_test

import (
	"testing"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/testutil"

	"gorm.io/gorm"
)

func openUsageAggregationRunnerDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.OpenTestDatabase(t)
}

func assertUsageAggregationCheckpoint(t *testing.T, db *gorm.DB, name string, want int64) {
	t.Helper()
	var checkpoint entities.UsageAggregationCheckpoint
	if err := db.Where("name = ?", name).Take(&checkpoint).Error; err != nil {
		t.Fatalf("load usage aggregation checkpoint %s: %v", name, err)
	}
	if checkpoint.LastAggregatedUsageEventID != want {
		t.Fatalf("expected usage aggregation checkpoint %s=%d, got %+v", name, want, checkpoint)
	}
}
