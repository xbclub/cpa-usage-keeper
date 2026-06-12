package repository

import (
	"testing"

	"cpa-usage-keeper/internal/entities"
)

func TestInsertBatchSizeUsesModelColumnCount(t *testing.T) {
	usageIdentityColumnCount := insertBatchColumnCount(entities.UsageIdentity{})
	usageIdentityBatchSize := insertBatchSize(entities.UsageIdentity{})
	// PostgreSQL 变量上限远高于旧版上限，宽模型的 batch size 由 maxRepositoryInsertBatchSize 封顶。
	expectedSize := min(maxRepositoryInsertBatchSize, pgVariableLimit/usageIdentityColumnCount)
	if usageIdentityBatchSize != expectedSize {
		t.Fatalf("expected usage identity batch size %d (pgVariableLimit=%d / columns=%d, capped at %d), got %d", expectedSize, pgVariableLimit, usageIdentityColumnCount, maxRepositoryInsertBatchSize, usageIdentityBatchSize)
	}

	narrowBatchSize := insertBatchSize(narrowInsertBatchModel{})
	if narrowBatchSize != maxRepositoryInsertBatchSize {
		t.Fatalf("expected narrow model to keep max batch size %d, got %d", maxRepositoryInsertBatchSize, narrowBatchSize)
	}
}

type narrowInsertBatchModel struct {
	Name string
}

func TestInsertBatchSizeCachesModelColumnCount(t *testing.T) {
	model := entities.UsageIdentity{}
	first := insertBatchSize(model)
	if insertBatchColumnCountCacheEntries() == 0 {
		t.Fatal("expected insert batch size to cache column count")
	}
	second := insertBatchSize(model)
	if second != first {
		t.Fatalf("expected cached batch size %d, got %d", first, second)
	}
}

func insertBatchColumnCountCacheEntries() int {
	count := 0
	insertBatchColumnCountCache.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}
