# Deep Interview Spec: 概览页 API Key 总用量 + 模型筛选

## Metadata
- Interview ID: di-overview-apikey-model-filter
- Rounds: 6
- Final Ambiguity Score: 12%
- Type: brownfield
- Generated: 2026-06-08
- Threshold: 0.2
- Threshold Source: default
- Status: PASSED

## Clarity Breakdown
| Dimension | Score | Weight | Weighted |
|-----------|-------|--------|----------|
| Goal Clarity | 0.93 | 0.35 | 0.326 |
| Constraint Clarity | 0.85 | 0.25 | 0.213 |
| Success Criteria | 0.83 | 0.25 | 0.208 |
| Context Clarity | 0.90 | 0.15 | 0.135 |
| **Total Clarity** | | | **0.881** |
| **Ambiguity** | | | **12%** |

## Topology
| Component | Status | Description |
|-----------|--------|-------------|
| API Key 汇总表格 | active | StatCards 下方显示所有 API Key 的请求/token/费用汇总表格 |
| 模型筛选器 | active | 概览页加模型下拉筛选，过滤全部概览数据，选项排除为 0 的模型 |

## Goal

在管理员概览页面 (UsagePage) 增加两个功能：

1. **API Key 汇总表格**：在 StatCards 下方增加一个表格，展示每个 API Key 在当前时间范围内的请求次数、token 用量、费用等汇总数据。同时保留现有 API Key 下拉筛选功能（选某个 Key 后概览全部数据只显示该 Key 的）。
2. **模型筛选器**：在筛选栏增加模型下拉选择，选中后概览的**全部数据**（统计卡片、图表、汇总表）都只展示该模型的数据。下拉选项排除当前时间范围内请求数为 0 的模型。

## Constraints

- 页面: UsagePage（管理后台 `/usage/overview`），**不涉及** KeyOverviewPage
- 模型筛选排除为 0 的选项：只影响筛选下拉列表，不影响汇总表格（汇总表仍按选中的 API Key 筛选展示）
- API Key 筛选下拉选项排除请求为 0 的 Key
- 复用现有的 API 基础设施和聚合数据（`Series.Models`、预聚合 hourly/daily stats）
- 后端 `UsageQueryFilter` 需增加 `Model` 字段，概览查询需支持按 model 过滤
- 前端风格与现有 StatCards、ServiceHealthCard 保持一致

## Non-Goals

- 不修改 KeyOverviewPage
- 不改变现有 ChartLineSelector 的行为
- 不新增数据库表或迁移（利用现有预聚合数据）

## Acceptance Criteria

- [ ] 后端 `UsageQueryFilter` 增加 `Model` 字段，概览 API 支持 `?model=xxx` 查询参数
- [ ] 后端新增概览模型筛选项 API（类似 `/usage/events/filters/models`），返回当前时间范围内请求 > 0 的模型列表
- [ ] 后端新增概览 API Key 汇总数据 API，返回每个 API Key 的请求/token/费用汇总
- [ ] 前端 UsagePage 筛选栏增加模型下拉，选中后概览全部数据（StatCards、图表、汇总表）按模型过滤
- [ ] 模型下拉选项只显示当前时间范围内有请求的模型（排除 0 值）
- [ ] 前端 StatCards 下方增加 API Key 汇总表格，显示每个 Key 的请求次数、token 数、费用
- [ ] API Key 下拉选项排除请求为 0 的 Key
- [ ] 筛选联动：选 API Key 或模型后，汇总表格和概览数据同步更新
- [ ] i18n：新增的 UI 文案支持中英日三语

## Technical Context

### 关键文件

**后端：**
- `internal/service/dto/usage.go` — `UsageFilter` 已有 `Model` 字段（用于 events），`UsageOverviewSnapshot` 已有 `Series.Models`
- `internal/repository/dto/usage_query_filter.go` — `UsageQueryFilter` 缺少 `Model` 字段，需添加
- `internal/service/usage.go:39` — `GetUsageOverview()` 目前不传 model 到 repository，需修改
- `internal/repository/usage.go` — `BuildUsageOverviewWithFilter()` 需支持 model 过滤
- `internal/api/usage_overview.go` — `/usage/overview` 路由需解析 `model` 查询参数
- `internal/api/usage_events.go` — `/usage/events/filters/models` 可参考模型筛选选项模式

**前端：**
- `web/src/pages/UsagePage.tsx` — 管理概览页主组件，需加模型下拉和汇总表格
- `web/src/components/usage/StatCards.tsx` — 汇总表格放在此组件下方
- `web/src/lib/api.ts:111` — `fetchUsageOverview()` 已有，需加 `model` 参数
- `web/src/lib/types.ts` — 需增加汇总表格类型定义
- `web/src/i18n/index.ts` — 新增 UI 文案

### 数据流

```
现有流程：
  Frontend → fetchUsageOverview(range, start, end, apiKeyId)
           → GET /usage/overview?range=8h&api_key_id=1
           → GetUsageOverview(filter) → BuildUsageOverviewWithFilter(db, queryFilter)
           → 预聚合表查询 (hourly/daily stats 按 api_group_key 过滤)

新增：
  1. 模型过滤：queryFilter 加 Model 字段 → 聚合查询 WHERE 条件加 model
  2. 模型筛选项：新增 API 类似 /usage/events/filters/models，从预聚合表查去重 model
  3. API Key 汇总：新增 API 或在 overview response 里加 per-key 汇总字段
```

## Interview Transcript
<details>
<summary>Full Q&A (6 rounds)</summary>

### Round 1
**Q:** 有两个概览页面：KeyOverviewPage（API Key 登录页）和 UsagePage（管理后台）。你想在哪个页面加？
**A:** UsagePage（管理后台）

### Round 2
**Q:** "API Key 总用量显示"具体指什么？现在已有 API Key 下拉筛选。
**A:** 两者都要：筛选过滤 + 汇总排行

### Round 3
**Q:** API Key 汇总排行想用什么形式展示？
**A:** 表格

### Round 4
**Q:** "模型选择"筛选的作用范围？
**A:** 过滤概览全部数据（StatCards、图表、汇总表都按模型过滤）

### Round 5
**Q:** "排除为 0 的 API Key 和模型"具体应用在哪？
**A:** 只排除筛选选项（下拉列表），汇总表仍显示所有

### Round 6
**Q:** API Key 汇总表格放在概览页面的哪个位置？
**A:** StatCards 下方

</details>
