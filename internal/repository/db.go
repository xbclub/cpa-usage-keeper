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

// CleanupStorage 是每日维护任务的统一仓储清理入口。
func CleanupStorage(db *gorm.DB, now time.Time) (dto.StorageCleanupResult, error) {
	redisResult, err := CleanupRedisUsageInbox(db, now)
	if err != nil {
		return dto.StorageCleanupResult{RedisInbox: redisResult}, err
	}
	if err := CleanupUsageOverviewHealthStats(db, now); err != nil {
		return dto.StorageCleanupResult{RedisInbox: redisResult}, err
	}
	return dto.StorageCleanupResult{RedisInbox: redisResult}, nil
}
