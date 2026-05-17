package service

import (
	"context"
	"testing"
	"time"

	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/testutil"
)

func TestUpdateCPAAPIKeyAliasAcceptsParsedInt64ID(t *testing.T) {
	db := testutil.OpenTestDatabase(t)
	if err := repository.SyncCPAAPIKeys(db, []string{"sk-alpha123456"}, time.Date(2026, 5, 13, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("seed API keys: %v", err)
	}
	provider := NewCPAAPIKeyService(db)

	row, err := provider.UpdateCPAAPIKeyAlias(context.Background(), int64(1), "Primary Key")
	if err != nil {
		t.Fatalf("UpdateCPAAPIKeyAlias returned error: %v", err)
	}
	if row.ID != 1 || row.KeyAlias != "Primary Key" {
		t.Fatalf("unexpected updated row: %+v", row)
	}
}
