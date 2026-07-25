package repository

import "cpa-usage-keeper/internal/pricing"

// emptyPricingResolverForTest 返回基于空快照的定价 resolver，供不需要真实价格数据的 repository 包测试使用。
// （#353 起 Build*WithFilter 系列函数新增 costResolver pricing.Resolver 末参。）
func emptyPricingResolverForTest() pricing.Resolver {
	return pricing.NewCatalog(pricing.EmptySnapshot()).NewResolver()
}
