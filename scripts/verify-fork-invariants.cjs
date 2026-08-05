#!/usr/bin/env node
// verify-fork-invariants.cjs — fork-unique 不变量校验,每次上游 merge 后必跑。
//
// 为什么存在:历史上 API Key Summary 序列化被 merge 静默覆盖(v1.13.6,藏 2 个月)、
// i18n zh-TW fork 键反复丢失(3 次)、4 个前端 t() 键三 locale 全缺 —— 这些都
// "测试套件全绿"但"功能实际坏了"。本脚本用机器判定抓住 merge 之外的结构性腐烂,
// 不依赖人工 review 是否仔细。
//
// 失败即 exit 1,配 Makefile `verify-fork-invariants` gate。
//
// 用法: node scripts/verify-fork-invariants.cjs

const fs = require('fs');
const path = require('path');
const { execSync } = require('child_process');

const ROOT = path.resolve(__dirname, '..');
const fail = (msg) => { console.error('❌ ' + msg); process.exitCode = 1; };
const ok = (msg) => { console.log('✅ ' + msg); };

// ---------- 1. 加载 i18n resources ----------
const i18nPath = path.join(ROOT, 'web/src/i18n/index.ts');
const src = fs.readFileSync(i18nPath, 'utf8');
const start = src.indexOf('const resources = {');
if (start < 0) { fail('找不到 resources 定义'); process.exit(1); }
let depth = 0, objStart = -1, objEnd = -1;
for (let i = start; i < src.length; i++) {
  if (src[i] === '{') { if (objStart < 0) objStart = i; depth++; }
  else if (src[i] === '}') { depth--; if (depth === 0) { objEnd = i + 1; break; } }
}
let resources;
try { resources = eval('(' + src.slice(objStart, objEnd) + ')'); }
catch (e) { fail('resources 解析失败: ' + e.message); process.exit(1); }
const LOCALES = Object.keys(resources);

function flatten(obj, prefix, out) {
  for (const k in obj) {
    const v = obj[k];
    const p = prefix ? prefix + '.' + k : k;
    if (v && typeof v === 'object' && !Array.isArray(v)) flatten(v, p, out);
    else out.add(p);
  }
}
const translationKeys = {};      // lng -> Set (translation.* 去前缀,匹配前端 t('a.b'))
const translationKeysPrefixed = {}; // lng -> Set (含 translation. 前缀)
for (const lng of LOCALES) {
  translationKeys[lng] = new Set();
  translationKeysPrefixed[lng] = new Set();
  if (resources[lng].translation) {
    flatten(resources[lng].translation, '', translationKeys[lng]);
    flatten(resources[lng], '', translationKeysPrefixed[lng]);
  }
}

// ---------- 2. locale 键对等 ----------
console.log('\n[1] i18n locale 键对等 (en/zh/zh-TW 同一翻译键集)');
{
  const ref = LOCALES[0];
  const refSet = translationKeys[ref];
  let clean = true;
  for (const lng of LOCALES) {
    const s = translationKeys[lng];
    const missing = [...refSet].filter(k => !s.has(k));
    const extra = [...s].filter(k => !refSet.has(k));
    if (missing.length) { fail(`${lng} 缺 ${missing.length} 键: ${missing.slice(0, 10).join(', ')}${missing.length > 10 ? ' ...' : ''}`); clean = false; }
    if (extra.length) { fail(`${lng} 多 ${extra.length} 键(其他 locale 没有): ${extra.slice(0, 10).join(', ')}`); clean = false; }
  }
  if (clean) ok(`三 locale 键集一致 (${refSet.size} 键)`);
}

// ---------- 3. fork-unique 键三 locale 必须存在 ----------
console.log('\n[2] fork-unique i18n 键三 locale 必须存在');
const FORK_I18N_KEYS = [
  'common.clear_cache', 'common.clear_cache_confirm',
  'usage_stats.model_filter', 'usage_stats.all_models',
  'usage_stats.api_key_summary_title', 'usage_stats.api_key',
  'usage_stats.model_filter_selected_one', 'usage_stats.model_filter_selected_other',
  'usage_stats.credentials_refresh_single',
];
{
  let clean = true;
  for (const key of FORK_I18N_KEYS) {
    for (const lng of LOCALES) {
      if (!translationKeys[lng].has(key)) { fail(`${lng} 缺 fork-unique 键 ${key}`); clean = false; }
    }
  }
  if (clean) ok(`${FORK_I18N_KEYS.length} 个 fork-unique 键三 locale 齐全`);
}

// ---------- 4. 前端 t() 引用 ⊆ i18n 定义(复数后缀感知) ----------
console.log('\n[3] 前端 t() 引用 ⊆ i18n 定义(复数后缀感知,排除 test/-only 负断言)');
const PLURAL_SUFFIXES = ['_zero', '_one', '_two', '_few', '_many', '_other'];
function keyResolves(key, lngSet) {
  if (lngSet.has(key)) return true;
  return PLURAL_SUFFIXES.some(s => lngSet.has(key + s));
}
{
  // 排除 test 文件:测试里常含 `.not.toContain('key')` 负断言或断言字符串,不是真实引用。
  const files = execSync("grep -rlE \"[^a-zA-Z_]t\\(['\\\"]\" web/src --include='*.ts' --include='*.tsx' | grep -vE '/test/|\\.test\\.'", { cwd: ROOT }).toString().split('\n').filter(Boolean);
  const used = new Map(); // key -> [files]
  const re = /[^a-zA-Z_]t\(\s*['"]([a-zA-Z0-9_.]+)['"]/g;
  for (const f of files) {
    const t = fs.readFileSync(path.join(ROOT, f), 'utf8');
    let m;
    while ((m = re.exec(t))) {
      if (!used.has(m[1])) used.set(m[1], []);
      used.get(m[1]).push(f);
    }
  }
  const enSet = translationKeys['en'];
  const missing = [];
  for (const [key, files] of used) {
    // 跳过明显非 i18n 命名空间(无点,或非已知顶层 ns)
    if (!key.includes('.')) continue;
    const topNs = key.split('.')[0];
    const knownNs = new Set();
    for (const lng of LOCALES) for (const k of translationKeys[lng]) knownNs.add(k.split('.')[0]);
    if (!knownNs.has(topNs)) continue;
    if (!keyResolves(key, enSet)) missing.push({ key, sample: files[0] });
  }
  if (missing.length) {
    fail(`前端引用但 i18n 未定义的键 (${missing.length}):`);
    missing.slice(0, 20).forEach(m => fail(`  ${m.key}  (← ${path.relative(ROOT, m.sample)})`));
    if (missing.length > 20) fail(`  ... 还有 ${missing.length - 20} 个`);
  } else {
    ok(`${used.size} 个 t() 引用全部可解析`);
  }
}

// ---------- 5. fork-unique 后端符号必须存在 ----------
console.log('\n[4] fork-unique 后端符号存在(grep)');
const FORK_SYMBOLS = [
  // API Key Summary 5 层链路(Step 4.5 #2 / Step 4.23 #14)
  ['internal/repository/usage.go', 'accumulateAPIKeySummaryFromOverview'],
  ['internal/repository/usage_apikey_summary.go', 'func newAPIKeySummaryAccumulator'],
  ['internal/repository/dto/usage_overview.go', 'APIKeySummary'],
  ['internal/service/dto/usage.go', 'APIKeySummary'],
  ['internal/service/usage.go', 'mapUsageOverviewAPIKeySummary'],
  ['internal/api/usage_overview.go', 'buildUsageOverviewAPIKeySummary'],
  ['internal/api/usage_overview.go', 'api_key_summary'],
  ['internal/repository/usage.go', 'model IN ?'],            // 多选 filter
  ['internal/api/usage_analysis.go', 'range_outside_recent_30_days'], // latency guard
];
{
  let clean = true;
  for (const [file, sym] of FORK_SYMBOLS) {
    const full = path.join(ROOT, file);
    if (!fs.existsSync(full)) { fail(`fork 符号目标文件缺失: ${file}`); clean = false; continue; }
    const t = fs.readFileSync(full, 'utf8');
    if (!t.includes(sym)) { fail(`${file} 丢失 fork 符号: "${sym}"`); clean = false; }
  }
  if (clean) ok(`${FORK_SYMBOLS.length} 个 fork-unique 后端符号全部存活`);
}

// ---------- 6. fork-only 守卫文件存在 ----------
console.log('\n[5] fork-only 守卫文件存在(防被 merge 抹掉后静默)');
const FORK_GUARDS = [
  'internal/api/fork_apikey_summary_test.go',
  'web/src/i18n/test/forkKeys.test.ts',
];
{
  let clean = true;
  for (const g of FORK_GUARDS) {
    if (!fs.existsSync(path.join(ROOT, g))) { fail(`fork 守卫文件缺失: ${g}`); clean = false; }
  }
  if (clean) ok(`${FORK_GUARDS.length} 个 fork-only 守卫文件就位`);
}

// ---------- 7. fork-unique / 上游 #392 样式 class 存在(UsagePage.module.scss) ----------
console.log('\n[6] UsagePage.module.scss 关键样式 class 存在(防 merge 静默丢样式)');
{
  const scss = fs.readFileSync(path.join(ROOT, 'web/src/pages/UsagePage.module.scss'), 'utf8');
  // signOut* 是 fork-unique 重置/退出 pill(@extend updateCheck);rankingScopeTransition/toolbarContextSlot 是 #392 上游同步。
  const REQUIRED = [
    'signOutSwitcher', 'signOutPill', 'signOutPillActive', 'signOutPillInner',
    'rankingScopeTransition', 'rankingScopeTransitionOpen', 'rankingScopeTransitionInner',
    'toolbarContextSlot', 'toolbarContextSlotImmediate',
    'apiKeyFilterGroup',  // fork-unique model filter 容器
  ];
  let clean = true;
  for (const cls of REQUIRED) {
    // SCSS 里可能 .cls 或 &cls(嵌套)或 @extend .cls;统一查 class 名出现。
    const re = new RegExp('(\\.|&)' + cls + '\\b');
    if (!re.test(scss)) { fail(`UsagePage.module.scss 缺 class: .${cls}`); clean = false; }
  }
  if (clean) ok(`${REQUIRED.length} 个关键样式 class 全部存在`);
}

// ---------- 8. keeper-card 卡片统一重构采用对齐上游 ----------
// 历史教训(Step 4.23):fork 反复落后上游"卡片统一重构"(本地 styles.xxx → 全局 keeper-card-*)。
// 本检查确保 fork 采用 keeper-card-surface 的组件数 >= 上游,防止再漏同步。
console.log('\n[7] keeper-card 卡片重构采用对齐上游(fork >= upstream)');
{
  const forkFiles = execSync("grep -rl 'keeper-card-surface' web/src --include='*.tsx'", { cwd: ROOT }).toString().split('\n').filter(Boolean).map(f => f.replace(/^web\/src\//, ''));
  // 上游基准(写入固定值,避免每次 git show):v1.14.2 后上游 7 个 .tsx 用 keeper-card-surface。
  const UPSTREAM_KEEPER_CARD_FILES = 7;
  if (forkFiles.length < UPSTREAM_KEEPER_CARD_FILES) {
    fail(`fork keeper-card-surface 采用 ${forkFiles.length} 文件 < 上游 ${UPSTREAM_KEEPER_CARD_FILES}(可能漏同步卡片统一重构)`);
  } else {
    ok(`fork keeper-card-surface 采用 ${forkFiles.length} 文件 >= 上游 ${UPSTREAM_KEEPER_CARD_FILES}`);
  }
}

// ---------- 9. fork-unique 前端特性存活(merge 反复丢,Step 4.5 #1/#9/#13) ----------
console.log('\n[8] fork-unique 前端特性存活(渲染/集成,非仅文件存在)');
{
  const checks = [
    // ApiKeySummaryTable:文件存在 + 在 UsagePage 渲染(后端 api_key_summary 有消费者)
    ['web/src/components/usage/ApiKeySummaryTable.tsx', 'export function ApiKeySummaryTable', 'ApiKeySummaryTable 组件'],
    ['web/src/pages/UsagePage.tsx', '<ApiKeySummaryTable', 'UsagePage 渲染 ApiKeySummaryTable'],
    ['web/src/components/usage/index.ts', "ApiKeySummaryTable", 'usage index 导出 ApiKeySummaryTable'],
    // Select tooltip(Step 4.5 #13):option label 有 title 防截断
    ['web/src/components/ui/Select.tsx', 'title={opt.label}', 'Select option tooltip'],
    // Combobox 在 PriceSettingsCard(Step 4.5 #9):模型名下拉+自由输入
    ['web/src/components/usage/PriceSettingsCard.tsx', '<Combobox', 'PriceSettingsCard 用 Combobox(模型名)'],
    // Select disabled option(Step 4.5 #11)
    ['web/src/components/ui/Select.tsx', 'disabled', 'Select disabled 选项'],
  ];
  let clean = true;
  for (const [file, sym, label] of checks) {
    const t = fs.readFileSync(path.join(ROOT, file), 'utf8');
    if (!t.includes(sym)) { fail(`${label}: 缺失 (${file} 无 "${sym}")`); clean = false; }
  }
  if (clean) ok(`${checks.length} 个 fork-unique 前端特性全部存活`);
}

// ---------- 收尾 ----------
if (process.exitCode) {
  console.error('\n❌ 不变量校验失败 —— 见上方明细');
} else {
  console.log('\n✅ 全部 fork 不变量通过');
}
process.exit(process.exitCode || 0);
