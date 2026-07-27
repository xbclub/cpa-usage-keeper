package repository

import (
	"context"
	"testing"

	"cpa-usage-keeper/internal/pricing"
	"gorm.io/gorm"
)

// emptyPricingResolverForTest 返回基于空快照的定价 resolver，供不需要真实价格数据的 repository 包测试使用。
// （#353 起 Build*WithFilter 系列函数新增 costResolver pricing.Resolver 末参。）
func emptyPricingResolverForTest() pricing.Resolver {
	return pricing.NewCatalog(pricing.EmptySnapshot()).NewResolver()
}

// pricingResolverFromDBForTest 从测试 DB 加载价格快照构建 resolver。
// 用于 seed 了价格的测试（empty resolver 看不到 seeded 价格）；空 DB 时等价 emptyPricingResolverForTest。
func pricingResolverFromDBForTest(t *testing.T, db *gorm.DB) pricing.Resolver {
	t.Helper()
	snapshot, err := LoadPricingSnapshot(context.Background(), db)
	if err != nil {
		t.Fatalf("load pricing snapshot: %v", err)
	}
	return pricing.NewCatalog(snapshot).NewResolver()
}
