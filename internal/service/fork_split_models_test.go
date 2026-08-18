package service

import "testing"

// TestSplitUsageModelsFilter 验证 fork-unique 多选 model 筛选的逗号拆分。
// 守卫动机:前端 fetchUsageOverview 把多选数组 join(",") 后放进 model 查询参数,
// service 必须拆回 Models 走仓储层 model IN 过滤(v1.12.0 合并时接线丢失约 2 个月,review 修复)。
func TestSplitUsageModelsFilter(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  []string
	}{
		{name: "空串返回 nil(不过滤)", value: "", want: nil},
		{name: "空白返回 nil", value: "   ", want: nil},
		{name: "单选拆出单元素", value: "gemini-2.5-pro", want: []string{"gemini-2.5-pro"}},
		{name: "多选拆开并去空白", value: " gemini-2.5-pro , claude-sonnet-4 ,,", want: []string{"gemini-2.5-pro", "claude-sonnet-4"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitUsageModelsFilter(tc.value)
			if len(got) != len(tc.want) {
				t.Fatalf("splitUsageModelsFilter(%q) = %v, want %v", tc.value, got, tc.want)
			}
			for index := range got {
				if got[index] != tc.want[index] {
					t.Fatalf("splitUsageModelsFilter(%q)[%d] = %q, want %q", tc.value, index, got[index], tc.want[index])
				}
			}
		})
	}
}
