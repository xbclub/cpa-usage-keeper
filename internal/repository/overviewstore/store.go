package overviewstore

import (
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

const overviewDimensionsPredicate = "bucket_start = ? AND api_group_key = ? AND model = ? AND auth_index = ? AND model_alias = ? AND service_tier = ? AND response_service_tier = ? AND reasoning_effort = ? AND endpoint = ? AND executor_type = ?"

// ApplyRows 用同一套五维唯一键把 hourly 和 daily 增量写入当前事务。
func ApplyRows(tx *gorm.DB, hourlyRows []entities.UsageOverviewHourlyStat, dailyRows []entities.UsageOverviewDailyStat, now time.Time) error {
	// hourly 必须全部成功，调用方才会继续提交 daily 和 checkpoint。
	for _, row := range hourlyRows {
		if err := applyHourlyRow(tx, row, now); err != nil {
			return err
		}
	}
	// daily 与 hourly 共享同一事务和累计公式，禁止出现单表已推进状态。
	for _, row := range dailyRows {
		if err := applyDailyRow(tx, row, now); err != nil {
			return err
		}
	}
	return nil
}

func applyHourlyRow(tx *gorm.DB, row entities.UsageOverviewHourlyStat, now time.Time) error {
	updates := tokenStatUpdates(row.RequestCount, row.SuccessCount, row.FailureCount, row.InputTokens, row.OutputTokens, row.ReasoningTokens, row.CachedTokens, row.CacheReadTokens, row.CacheCreationTokens, row.TotalTokens, now)
	args := hourlyDimensionArgs(row)
	// update-first 避免正常累计走唯一索引冲突路径并消耗自增 ID。
	result := tx.Model(&entities.UsageOverviewHourlyStat{}).Where(overviewDimensionsPredicate, args...).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("update usage overview hourly stat: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		return nil
	}

	row.CreatedAt = timeutil.NormalizeStorageTime(now)
	row.UpdatedAt = timeutil.NormalizeStorageTime(now)
	// 并发创建相同 key 时 INSERT 撞唯一索引；PG 在事务内任何错误后中止整事务（SQLSTATE 25P02），
	// 直接重试 UPDATE 会失败。SAVEPOINT 隔离失败 INSERT，ROLLBACK 后事务恢复可重试（SQLite 同样支持）。
	return applyRowWithInsertRetry(tx, "sp_overview_hourly_insert",
		func() error { return tx.Create(&row).Error },
		func() (int64, error) {
			retry := tx.Model(&entities.UsageOverviewHourlyStat{}).Where(overviewDimensionsPredicate, args...).Updates(updates)
			return retry.RowsAffected, retry.Error
		})
}

func applyDailyRow(tx *gorm.DB, row entities.UsageOverviewDailyStat, now time.Time) error {
	updates := tokenStatUpdates(row.RequestCount, row.SuccessCount, row.FailureCount, row.InputTokens, row.OutputTokens, row.ReasoningTokens, row.CachedTokens, row.CacheReadTokens, row.CacheCreationTokens, row.TotalTokens, now)
	args := dailyDimensionArgs(row)
	// daily 使用与 hourly 完全相同的最终唯一键和 update-first 语义。
	result := tx.Model(&entities.UsageOverviewDailyStat{}).Where(overviewDimensionsPredicate, args...).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("update usage overview daily stat: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		return nil
	}

	row.CreatedAt = timeutil.NormalizeStorageTime(now)
	row.UpdatedAt = timeutil.NormalizeStorageTime(now)
	return applyRowWithInsertRetry(tx, "sp_overview_daily_insert",
		func() error { return tx.Create(&row).Error },
		func() (int64, error) {
			retry := tx.Model(&entities.UsageOverviewDailyStat{}).Where(overviewDimensionsPredicate, args...).Updates(updates)
			return retry.RowsAffected, retry.Error
		})
}

// applyRowWithInsertRetry 在 update-first 找不到行时 INSERT；并发冲突用 SAVEPOINT 隔离失败 INSERT 后重试 UPDATE。
// PG 在事务内任何错误后中止整事务（SQLSTATE 25P02），直接重试 UPDATE 会失败；
// ROLLBACK TO SAVEPOINT 清除中止状态使重试可行（SQLite 也支持 SAVEPOINT，行为一致）。
// savepoint 名调用方保证在当前事务内不与并发活跃的同名 savepoint 冲突（ApplyRows 顺序执行，每行 RELEASE 后才进入下一行）。
func applyRowWithInsertRetry(tx *gorm.DB, savepoint string, insert func() error, retryUpdate func() (int64, error)) error {
	if err := tx.Exec("SAVEPOINT " + savepoint).Error; err != nil {
		return fmt.Errorf("savepoint %s: %w", savepoint, err)
	}
	insertErr := insert()
	if insertErr == nil {
		_ = tx.Exec("RELEASE SAVEPOINT " + savepoint).Error
		return nil
	}
	// INSERT 冲突（并发）：回滚到 savepoint 清除事务中止状态，再重试一次完整五维 UPDATE。
	if rbErr := tx.Exec("ROLLBACK TO SAVEPOINT " + savepoint).Error; rbErr != nil {
		return fmt.Errorf("insert usage stat: %w; rollback to savepoint %s: %v", insertErr, savepoint, rbErr)
	}
	affected, retryErr := retryUpdate()
	_ = tx.Exec("RELEASE SAVEPOINT " + savepoint).Error
	if retryErr != nil {
		return fmt.Errorf("insert usage stat: %w; retry update: %v", insertErr, retryErr)
	}
	if affected == 0 {
		return fmt.Errorf("insert usage stat: %w; retry update matched no existing row", insertErr)
	}
	return nil
}

func hourlyDimensionArgs(row entities.UsageOverviewHourlyStat) []any {
	return []any{
		timeutil.FormatStorageTime(row.BucketStart), row.APIGroupKey, row.Model, row.AuthIndex, row.ModelAlias,
		row.ServiceTier, row.ResponseServiceTier, row.ReasoningEffort, row.Endpoint, row.ExecutorType,
	}
}

func dailyDimensionArgs(row entities.UsageOverviewDailyStat) []any {
	return []any{
		timeutil.FormatStorageTime(row.BucketStart), row.APIGroupKey, row.Model, row.AuthIndex, row.ModelAlias,
		row.ServiceTier, row.ResponseServiceTier, row.ReasoningEffort, row.Endpoint, row.ExecutorType,
	}
}

func tokenStatUpdates(requestCount, successCount, failureCount, inputTokens, outputTokens, reasoningTokens, cachedTokens, cacheReadTokens, cacheCreationTokens, totalTokens int64, now time.Time) map[string]any {
	return map[string]any{
		"request_count":         gorm.Expr("request_count + ?", requestCount),
		"success_count":         gorm.Expr("success_count + ?", successCount),
		"failure_count":         gorm.Expr("failure_count + ?", failureCount),
		"input_tokens":          gorm.Expr("input_tokens + ?", inputTokens),
		"output_tokens":         gorm.Expr("output_tokens + ?", outputTokens),
		"reasoning_tokens":      gorm.Expr("reasoning_tokens + ?", reasoningTokens),
		"cached_tokens":         gorm.Expr("cached_tokens + ?", cachedTokens),
		"cache_read_tokens":     gorm.Expr("cache_read_tokens + ?", cacheReadTokens),
		"cache_creation_tokens": gorm.Expr("cache_creation_tokens + ?", cacheCreationTokens),
		"total_tokens":          gorm.Expr("total_tokens + ?", totalTokens),
		"updated_at":            timeutil.FormatStorageTime(now),
	}
}
