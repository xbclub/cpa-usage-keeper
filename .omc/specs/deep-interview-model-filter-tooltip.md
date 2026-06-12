# Deep Interview Spec: Model Filter Dropdown Tooltip

## Metadata
- Interview ID: di-tooltip-model-filter-20260609
- Rounds: 1
- Final Ambiguity Score: 19.3%
- Type: brownfield
- Generated: 2026-06-09
- Threshold: 0.2
- Threshold Source: default
- Initial Context Summarized: no
- Status: PASSED

## Clarity Breakdown
| Dimension | Score | Weight | Weighted |
|-----------|-------|--------|----------|
| Goal Clarity | 0.85 | 0.35 | 0.298 |
| Constraint Clarity | 0.90 | 0.25 | 0.225 |
| Success Criteria | 0.60 | 0.25 | 0.150 |
| Context Clarity | 0.90 | 0.15 | 0.135 |
| **Total Clarity** | | | **0.808** |
| **Ambiguity** | | | **19.3%** |

## Topology
| Component | Status | Description | Coverage |
|-----------|--------|-------------|----------|
| Tooltip Component | active | Native HTML title attribute on Select option labels | Covered — use `title={opt.label}` on the `<span>` in Select.tsx |
| Model Filter Integration | active | Model filter dropdown shows full model name on hover | Covered — inherits from Select.tsx change automatically |

## Goal
Add a native HTML `title` attribute to the option label `<span>` in `Select.tsx` so that when a model name (or any option text) is too long to display in the dropdown, users can hover over it to see the full name via the browser's built-in tooltip.

## Constraints
- Use **native HTML `title` attribute only** — no custom tooltip component, no extra dependencies
- Single-line change in `Select.tsx` — no new files or components needed
- The `title` is always present on all Select options (not conditional on truncation), which is harmless: if the text fits, the tooltip just shows the same text
- Applies to **all** Select instances automatically (model filter, API key filter, etc.)

## Non-Goals
- Custom-styled tooltip with branded appearance or animations
- Smart truncation detection (showing tooltip only when text is actually clipped)
- Tooltip on the trigger button (closed state) — only the dropdown options need this

## Acceptance Criteria
- [ ] Each `<span className={styles.optionLabel}>` in Select.tsx has `title={opt.label}`
- [ ] Hovering over a truncated model name in the model filter dropdown shows the full name
- [ ] Other Select instances (API key filter, time range, etc.) also gain the tooltip automatically
- [ ] No visual regression — dropdown styling unchanged

## Technical Context
**File to modify:** `web/src/components/ui/Select.tsx`

**Current code (line 219):**
```tsx
<span className={styles.optionLabel}>{opt.label}</span>
```

**Target code:**
```tsx
<span className={styles.optionLabel} title={opt.label}>{opt.label}</span>
```

The dropdown is rendered via `createPortal(dropdown, document.body)` (line 265), but native `title` works correctly on portaled content — no special handling needed.

The `SelectOption` interface (line 15-21) already has `label: string` which is the text to display and use as the title.

## Assumptions Exposed & Resolved
| Assumption | Challenge | Resolution |
|------------|-----------|------------|
| Need a custom tooltip component | Native `title` attribute is sufficient for showing full text on hover | User confirmed: use native `title` — simplest approach |
| Only model filter needs tooltip | Other Select instances may also have long text | Apply to all Select options — one change covers everything |

## Ontology (Key Entities)
| Entity | Type | Fields | Relationships |
|--------|------|--------|---------------|
| Select | core domain | options, value, onChange, open | contains SelectOptions |
| SelectOption | core domain | value, label, title | rendered inside Select |
| Model Filter | supporting | overviewModelFilter | uses Select component |

## Ontology Convergence
| Round | Entity Count | New | Changed | Stable | Stability Ratio |
|-------|-------------|-----|---------|--------|----------------|
| 1 | 3 | 3 | - | - | N/A |

## Interview Transcript
<details>
<summary>Full Q&A (1 round)</summary>

### Round 1
**Q:** 你希望用哪种方式实现 tooltip？原生 title 属性、自定义 Tooltip 组件、还是智能检测截断？
**A:** 原生 title 属性 — 最简洁的方案，无需额外依赖
**Ambiguity:** 19.3% (Goal: 0.85, Constraints: 0.90, Criteria: 0.60, Context: 0.90)

</details>
