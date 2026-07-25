package test

import "cpa-usage-keeper/internal/pricing"

// emptyPricingCatalogForTest 返回基于空快照的定价 catalog，供不需要真实价格数据的 api/test 子包测试使用。
// （#353 起 NewUsageService 系列构造函数新增 pricingCatalog 参数。）
func emptyPricingCatalogForTest() *pricing.Catalog {
	return pricing.NewCatalog(pricing.EmptySnapshot())
}
