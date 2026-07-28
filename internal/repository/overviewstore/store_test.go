package overviewstore

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/testutil"
	"cpa-usage-keeper/internal/timeutil"

	"gorm.io/gorm"
)

// TestApplyHourlyRowRetriesUpdateAfterInsertConflict 验证 INSERT 撞唯一索引(并发竞争)时,
// SAVEPOINT 隔离失败 INSERT 使重试 UPDATE 可行。PG 在事务内任何错误后中止整事务(SQLSTATE 25P02),
// 直接重试 UPDATE 会失败;无 SAVEPOINT 的旧实现即此 bug(生产 23505 → 25P02)。
//
// 用 BeforeCreate 回调模拟 INSERT 冲突(对 HourlyStat create 强制失败),断言:
//   1. 重试 UPDATE 跑通(返回 "matched no existing row",而非 25P02 "transaction aborted");
//   2. 事务恢复,后续查询正常(不残留 aborted 状态)。
func TestApplyHourlyRowRetriesUpdateAfterInsertConflict(t *testing.T) {
	db := testutil.OpenTestDatabase(t)
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	row := entities.UsageOverviewHourlyStat{
		BucketStart: now, APIGroupKey: "provider-a", Model: "claude", AuthIndex: "auth-a",
		RequestCount: 5, TotalTokens: 50,
	}

	// 模拟并发竞争:HourlyStat 的 create 强制报错(等价撞唯一索引 23505)。
	cbName := "test:force_hourly_insert_conflict"
	db.Callback().Create().Before("gorm:create").Register(cbName, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Model.(*entities.UsageOverviewHourlyStat); ok {
			tx.AddError(fmt.Errorf("simulated duplicate key conflict"))
		}
	})
	t.Cleanup(func() { _ = db.Callback().Create().Remove(cbName) })

	// update-first 找不到行 → INSERT 撞回调失败 → SAVEPOINT ROLLBACK → 重试 UPDATE。
	err := db.Transaction(func(tx *gorm.DB) error {
		return ApplyRows(tx, []entities.UsageOverviewHourlyStat{row}, nil, now)
	})
	if err == nil {
		t.Fatal("expected insert-conflict error after simulated concurrent insert, got nil")
	}
	// 关键断言:重试 UPDATE 不能因事务中止(25P02)失败,必须跑通(只是没找到行)。
	if strings.Contains(err.Error(), "current transaction is aborted") || strings.Contains(err.Error(), "25P02") {
		t.Fatalf("SAVEPOINT should have recovered the transaction, but retry hit aborted-state: %v", err)
	}
	if !strings.Contains(err.Error(), "retry update matched no existing row") {
		t.Fatalf("expected 'retry update matched no existing row' after SAVEPOINT recovery, got: %v", err)
	}

	// 事务恢复:后续查询正常(无残留 aborted 状态;冲突路径未真正插入行)。
	var count int64
	if err := db.Model(&entities.UsageOverviewHourlyStat{}).
		Where("bucket_start = ?", timeutil.FormatStorageTime(now)).Count(&count).Error; err != nil {
		t.Fatalf("post-conflict count query failed (transaction not recovered?): %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no hourly stat persisted on conflict path, got %d rows", count)
	}
}

// TestApplyHourlyRowNormalUpdateAndInsert 验证 update-first 正常路径:已有行 UPDATE,新行 INSERT。
func TestApplyHourlyRowNormalUpdateAndInsert(t *testing.T) {
	db := testutil.OpenTestDatabase(t)
	now := time.Date(2026, 7, 28, 13, 0, 0, 0, time.UTC)
	row := entities.UsageOverviewHourlyStat{
		BucketStart: now, APIGroupKey: "provider-b", Model: "gpt", AuthIndex: "auth-b",
		RequestCount: 3, SuccessCount: 3, TotalTokens: 30,
	}

	// 第一次:新行 → INSERT。
	if err := db.Transaction(func(tx *gorm.DB) error {
		return ApplyRows(tx, []entities.UsageOverviewHourlyStat{row}, nil, now)
	}); err != nil {
		t.Fatalf("first ApplyRows (insert): %v", err)
	}

	// 第二次:同一维度 → UPDATE 累加。
	row.RequestCount = 2
	row.TotalTokens = 20
	if err := db.Transaction(func(tx *gorm.DB) error {
		return ApplyRows(tx, []entities.UsageOverviewHourlyStat{row}, nil, now)
	}); err != nil {
		t.Fatalf("second ApplyRows (update): %v", err)
	}

	var stored entities.UsageOverviewHourlyStat
	if err := db.Where("api_group_key = ? AND model = ?", "provider-b", "gpt").First(&stored).Error; err != nil {
		t.Fatalf("load stored hourly stat: %v", err)
	}
	if stored.RequestCount != 5 || stored.TotalTokens != 50 {
		t.Fatalf("expected accumulated request_count=5 total_tokens=50, got %+v", stored)
	}
}
