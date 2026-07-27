package test

import "cpa-usage-keeper/internal/pricing"

// emptyPricingCatalogForTest 返回基于空快照的定价 catalog，供不需要真实价格数据的 quota/test 子包测试使用。
// （#353 起 NewServiceWithRegistry/NewServiceWithRegistryAndOptions 需要 *pricing.Catalog，nil 会 panic。）
func emptyPricingCatalogForTest() *pricing.Catalog {
	return pricing.NewCatalog(pricing.EmptySnapshot())
}
