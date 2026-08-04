package api

// 本文件是 fork-only 守卫测试，文件名 fork_apikey_summary_test.go 是上游永远不会有的名字。
//
// 为什么单独放一个文件:API Key Summary 是 fork-unique 特性，横跨 repo accumulator →
// service DTO → api response 三层。历史上(bf8ba13 加 → bf6bcb3 v1.13.6 丢 → da99ae8 再加)
// 它每被上游 overview 管线重构覆盖一次就断，而且回归测试若和被守卫代码同处 usage_overview_test.go，
// 会被同一次 git checkout/merge 一起抹掉，守卫根本没机会 fail-loud —— bug 藏了 2 个月。
//
// 把守卫隔离到这个 fork-only 文件后，任何 `git checkout upstream/main -- internal/api/usage_overview_test.go`
// 或 `git merge-file` 都碰不到它。feature 代码一旦再被 merge 盖掉，这个文件里的测试会立刻失败报警。
//
// 每次 merge overview 管线后，务必 `go test ./internal/api/ -run APIKeySummary` 跑这两个守卫。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cpa-usage-keeper/internal/repository/dto"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

// TestUsageOverviewSerializesAPIKeySummaryWithRedaction 守护 fork-unique 的 API Key Summary
// 端到端序列化链路(service DTO → api 响应)。v1.13.6 曾丢失此字段(api_key_summary 不出现在
// 响应里，前端 ApiKeySummaryTable 永远空)。本测试确保响应恒有 api_key_summary，且 api_key 脱敏。
func TestUsageOverviewSerializesAPIKeySummaryWithRedaction(t *testing.T) {
	rawGroupKey := "sk-live-secret-key-abcdef"
	provider := &usageFilterStub{overview: &servicedto.UsageOverviewSnapshot{
		Usage: &dto.StatisticsSnapshot{TotalRequests: 5},
		APIKeySummary: []servicedto.UsageOverviewAPIKeySummary{
			{APIGroupKey: rawGroupKey, RequestCount: 3, TotalTokens: 1000, OutputTokens: 600},
		},
	}}
	router := NewRouter(nil, nil, provider, nil, AuthConfig{}, nil, "")

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview?range=24h", nil)
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	raw, ok := body["api_key_summary"]
	if !ok {
		t.Fatal("expected api_key_summary key in response (fork-unique field must always serialize)")
	}
	var rows []usageOverviewAPIKeySummary
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("unmarshal api_key_summary: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].APIKey == rawGroupKey {
		t.Fatal("expected api_key to be redacted, got raw group key (RedactSensitiveValue not applied)")
	}
	if rows[0].RequestCount != 3 || rows[0].TotalTokens != 1000 || rows[0].OutputTokens != 600 {
		t.Fatalf("unexpected row payload: %+v", rows[0])
	}
}

// TestUsageOverviewNilProviderStillSerializesEmptyAPIKeySummary 确保 usageProvider=nil 时
// api_key_summary 序列化为 []（非 null），前端字段始终存在。
func TestUsageOverviewNilProviderStillSerializesEmptyAPIKeySummary(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil, AuthConfig{}, nil, "")
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/overview?range=24h", nil)
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	raw, ok := body["api_key_summary"]
	if !ok {
		t.Fatal("expected api_key_summary key even when provider is nil")
	}
	if string(raw) != "[]" {
		t.Fatalf("expected api_key_summary == [], got %s", string(raw))
	}
}
