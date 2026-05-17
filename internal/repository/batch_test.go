package repository

import (
	"testing"
)

func TestInsertBatchSizeReturnsDefaultBatchSize(t *testing.T) {
	if insertBatchSize() != defaultInsertBatchSize {
		t.Fatalf("expected insertBatchSize to return defaultInsertBatchSize %d", defaultInsertBatchSize)
	}
}
