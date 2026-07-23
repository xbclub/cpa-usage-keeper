package repository

import (
	"fmt"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
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

// CleanupStorage 是每日维护任务的统一仓储清理入口：先清 Redis inbox 和过期 usage_events，再清 Overview health 细粒度统计。
// CleanupStorageOptions 控制每日维护任务的清理范围。
type CleanupStorageOptions struct {
	CleanupUsageEvents bool
}

func CleanupStorage(db *gorm.DB, now time.Time, options ...CleanupStorageOptions) (dto.StorageCleanupResult, error) {
	opts := CleanupStorageOptions{}
	if len(options) > 0 {
		opts = options[0]
	}
	redisResult, err := CleanupRedisUsageInbox(db, now)
	if err != nil {
		return dto.StorageCleanupResult{RedisInbox: redisResult}, err
	}
	var usageEventsDeleted int64
	if opts.CleanupUsageEvents {
		usageEventsDeleted, err = CleanupUsageEvents(db, now)
		if err != nil {
			return dto.StorageCleanupResult{RedisInbox: redisResult, UsageEventsDeleted: usageEventsDeleted}, err
		}
	}
	// PostgreSQL 不需要 VACUUM（由 autovacuum 自动维护）。
	return dto.StorageCleanupResult{RedisInbox: redisResult, UsageEventsDeleted: usageEventsDeleted}, nil
}

// CleanupUsageEvents 删除当前页面查询窗口外的原始 usage_events，保留从上个月 1 日本地零点开始的数据。
func CleanupUsageEvents(db *gorm.DB, now time.Time) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("database is nil")
	}
	cutoff := usageEventsCleanupCutoff(now)
	result := db.Unscoped().Where("timestamp < ?", timeutil.FormatStorageTime(cutoff)).Delete(&entities.UsageEvent{})
	if result.Error != nil {
		return result.RowsAffected, fmt.Errorf("cleanup usage events: %w", result.Error)
	}
	return result.RowsAffected, nil
}

func usageEventsCleanupCutoff(now time.Time) time.Time {
	localNow := now.In(time.Local)
	currentMonthStart := time.Date(localNow.Year(), localNow.Month(), 1, 0, 0, 0, 0, time.Local)
	return currentMonthStart.AddDate(0, -1, 0)
}
