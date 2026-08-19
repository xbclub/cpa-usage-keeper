package repository

import (
	"fmt"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/logging"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// OpenDatabase 创建 GORM PostgreSQL 连接并执行 AutoMigrate。
func OpenDatabase(cfg config.Config) (*gorm.DB, error) {
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	db, err := gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{
		Logger:  logging.NewGORMLogger(),
		NowFunc: func() time.Time { return timeutil.NormalizeStorageTime(time.Now()) },
	})
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("configure postgres database: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.DBMaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.DBMaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.DBConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(cfg.DBConnMaxIdleTime)

	// fork 没有 schema migration 框架，AutoMigrate 之前先手动补一次性迁移：
	// 把 redis_usage_inboxes.queue_key 收敛为 source。必须先于 AutoMigrate 执行，
	// 否则 AutoMigrate 会在旧数据行上添加 NOT NULL source 列而失败。
	if err := migrateRedisInboxQueueKeyToSource(db); err != nil {
		return nil, fmt.Errorf("migrate redis_usage_inboxes queue_key to source: %w", err)
	}
	// v1.13.6 activity 子系统给 overview stats 加了 5 维字段(service_tier 等)，唯一索引
	// 从 5 列(bucket/api/model/auth/alias)扩到 10 列(dimensions)。AutoMigrate 创建了新索引
	// 但不删旧的 → 旧 5 列索引仍在，INSERT 撞旧索引(5 列相同即冲突)，而 update-first/重试
	// UPDATE 用 10 列谓词不匹配 → 聚合 "matched no existing row"。必须先删旧索引再 AutoMigrate。
	if err := dropLegacyOverviewStatUniqueIndexes(db); err != nil {
		return nil, fmt.Errorf("drop legacy overview stat unique indexes: %w", err)
	}

	if err := db.AutoMigrate(entities.All()...); err != nil {
		return nil, fmt.Errorf("auto migrate database: %w", err)
	}

	return db, nil
}

// InsertUsageEvents 按 Redis inbox 消费结果逐条落库；request_id/event_key 重复也保留为独立事件。
func InsertUsageEvents(db *gorm.DB, events []entities.UsageEvent) (int, int, error) {
	if db == nil {
		return 0, 0, fmt.Errorf("database is nil")
	}
	if len(events) == 0 {
		return 0, 0, nil
	}

	inserted := 0

	err := db.Transaction(func(tx *gorm.DB) error {
		for start := 0; start < len(events); start += insertBatchSize(entities.UsageEvent{}) {
			end := min(start+insertBatchSize(entities.UsageEvent{}), len(events))
			batch := events[start:end]
			for index := range batch {
				batch[index].Timestamp = timeutil.NormalizeStorageTime(batch[index].Timestamp)
			}

			result := tx.Create(&batch)
			if result.Error != nil {
				return fmt.Errorf("insert usage events: %w", result.Error)
			}
			inserted += int(result.RowsAffected)
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}

	return inserted, 0, nil
}

// migrateRedisInboxQueueKeyToSource 把历史 queue_key 列替换为可直接落库来源名的 source 列。
// fork 使用 AutoMigrate 而非上游的 schema migration 框架，所以在此手动补这一步迁移逻辑，
// 等价于上游 migration 20260612_redis_inbox_source。对已经是 source-only 的新库是 no-op。
// 必须在 AutoMigrate 之前调用：entity 已定义 NOT NULL source 列，AutoMigrate 在有旧数据时会
// 因 NOT NULL 约束失败，所以这里先把数据迁好、把 queue_key 列删掉。
func migrateRedisInboxQueueKeyToSource(db *gorm.DB) error {
	// 旧库如果没有 redis_usage_inboxes 表，说明对应功能未初始化，本迁移不需要做任何结构修正。
	if !db.Migrator().HasTable("redis_usage_inboxes") {
		return nil
	}
	// source 列已存在时，说明迁移已完成，直接返回（保证幂等）。
	if db.Migrator().HasColumn("redis_usage_inboxes", "source") {
		return nil
	}
	// source 不存在时先添加可空列，再按 queue_key 回填完整来源名。
	// 历史 queue_key（如 "usage"/"queue"）无法精确反推出 subscribe/redis/http 来源，
	// 统一记为 redis_pull:<queue_key>，与 #210 引入的 redis_pull source 前缀对齐。
	if err := db.Exec("ALTER TABLE redis_usage_inboxes ADD COLUMN source TEXT").Error; err != nil {
		return fmt.Errorf("add redis_usage_inboxes.source column: %w", err)
	}
	if db.Migrator().HasColumn("redis_usage_inboxes", "queue_key") {
		if err := db.Exec("UPDATE redis_usage_inboxes SET source = 'redis_pull:' || queue_key WHERE source IS NULL").Error; err != nil {
			return fmt.Errorf("backfill redis_usage_inboxes.source from queue_key: %w", err)
		}
	}
	// 缺省值兜底：没有 queue_key 可回填的行统一标记 unknown。
	if err := db.Exec("UPDATE redis_usage_inboxes SET source = 'unknown' WHERE source IS NULL").Error; err != nil {
		return fmt.Errorf("backfill redis_usage_inboxes.source default: %w", err)
	}
	// 删除历史 queue_key 索引，避免 DROP COLUMN 时被索引依赖阻塞。
	if err := db.Exec("DROP INDEX IF EXISTS idx_redis_usage_inboxes_queue_key").Error; err != nil {
		return fmt.Errorf("drop redis_usage_inboxes queue_key index: %w", err)
	}
	// queue_key 存在时才删除，保证迁移可重复执行并兼容已经部分升级的数据库。
	if db.Migrator().HasColumn("redis_usage_inboxes", "queue_key") {
		if err := db.Exec("ALTER TABLE redis_usage_inboxes DROP COLUMN queue_key").Error; err != nil {
			return fmt.Errorf("drop redis_usage_inboxes.queue_key column: %w", err)
		}
	}
	return nil
}

// dropLegacyOverviewStatUniqueIndexes 删除 v1.13.6 activity 子系统之前遗留的 5 列唯一索引。
// 旧索引建在 (bucket_start, api_group_key, model, auth_index, model_alias) 上（GORM 按列名自动命名为
// ..._bucket_api_model_auth_alias）；加 5 维字段后 entity 改用 10 列 ..._dimensions 索引，AutoMigrate
// 只新增不删除 → 两个索引共存。旧索引会让 INSERT 撞 5 列（5 维不同也冲突），而 update-first/重试 UPDATE
// 用 10 列谓词不匹配，导致聚合 "matched no existing row"。这里在 AutoMigrate 前删掉旧索引。
func dropLegacyOverviewStatUniqueIndexes(db *gorm.DB) error {
	legacyIndexes := []string{
		"uniq_usage_overview_hourly_stats_bucket_api_model_auth_alias",
		"uniq_usage_overview_daily_stats_bucket_api_model_auth_alias",
	}
	for _, name := range legacyIndexes {
		if err := db.Exec(fmt.Sprintf("DROP INDEX IF EXISTS %s", name)).Error; err != nil {
			return fmt.Errorf("drop legacy index %s: %w", name, err)
		}
	}
	return nil
}

// CleanupStorage 是每日维护任务的统一仓储入口：先清 inbox、归档 raw events。
// PostgreSQL 不需要 VACUUM（由 autovacuum 自动维护）。
func CleanupStorage(db *gorm.DB, now time.Time) (dto.StorageCleanupResult, error) {
	redisResult, err := CleanupRedisUsageInbox(db, now)
	if err != nil {
		return dto.StorageCleanupResult{RedisInbox: redisResult}, err
	}
	usageEventsArchive, err := ArchiveExpiredUsageEvents(databaseContext(db), db, now)
	result := dto.StorageCleanupResult{
		RedisInbox:               redisResult,
		UsageEventsArchived:      usageEventsArchive.Archived,
		UsageEventsArchiveStatus: usageEventsArchive.Status,
	}
	if err != nil {
		return result, err
	}
	// Activity 的 short/medium/long 分别按自身 retention 清理，daily 永久保留。
	if err := CleanupUsageActivityStats(db, now); err != nil {
		return result, err
	}
	// Latency 小时保留 3 天、自然日保留 365 天(fork 无 VACUUM,PG 由 autovacuum 处理)。
	if err := CleanupUsageLatencyStats(db, now); err != nil {
		return result, err
	}
	return result, nil
}

const usageEventsRetentionDays = 90

func usageEventsArchiveCutoff(now time.Time) time.Time {
	localNow := now.In(time.Local)
	localDayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.Local)
	return localDayStart.AddDate(0, 0, -usageEventsRetentionDays)
}

// usageEventAggregationsCaughtUp 检查三类全局 checkpoint + Identity cursor 是否都追平到当前最大 event ID。
// 只有聚合全部追上后才允许把原始事件归档移出 hot 表。
func usageEventAggregationsCaughtUp(tx *gorm.DB) (bool, error) {
	var maxEventID int64
	if err := tx.Model(&entities.UsageEvent{}).Select("COALESCE(MAX(id), 0)").Scan(&maxEventID).Error; err != nil {
		return false, fmt.Errorf("load usage event archive watermark: %w", err)
	}
	if maxEventID == 0 {
		return true, nil
	}

	var checkpoints []entities.UsageAggregationCheckpoint
	names := []entities.UsageAggregationCheckpointName{
		entities.UsageAggregationCheckpointOverview,
		entities.UsageAggregationCheckpointActivity,
		entities.UsageAggregationCheckpointLatency,
	}
	if err := tx.Where("name IN ?", names).Find(&checkpoints).Error; err != nil {
		return false, fmt.Errorf("load usage aggregation archive watermarks: %w", err)
	}
	ready := make(map[entities.UsageAggregationCheckpointName]bool, len(names))
	for _, checkpoint := range checkpoints {
		ready[checkpoint.Name] = checkpoint.LastAggregatedUsageEventID >= maxEventID
	}
	for _, name := range names {
		if !ready[name] {
			return false, nil
		}
	}

	pendingIdentity, err := hasPendingUsageIdentityAggregation(tx)
	if err != nil {
		return false, fmt.Errorf("check identity archive watermark: %w", err)
	}
	return !pendingIdentity, nil
}
