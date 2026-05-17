package repository

import (
	"testing"

	"cpa-usage-keeper/internal/entities"
)

func TestInsertBatchSizeReturnsDefaultBatchSize(t *testing.T) {
	if insertBatchSize(entities.UsageIdentity{}) != defaultInsertBatchSize {
		t.Fatalf("expected insertBatchSize to return defaultInsertBatchSize %d", defaultInsertBatchSize)
	}
}
