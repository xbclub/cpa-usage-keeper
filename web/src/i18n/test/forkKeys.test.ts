import { describe, expect, it } from 'vitest';
import i18n from '../index';

// 本文件是 fork-only 守卫测试，文件名 forkKeys.test.ts 是上游永远不会有的名字。
//
// 为什么单独放一个文件:fork-unique 的 i18n 键历史上反复在 merge 后从 zh-TW 丢失
// （Step 4.12 #3 / 4.17 #5 / v1.14.2 第 3 次），且偶发 zh↔zh-TW 值错位。若守卫放
// i18n/index.test.ts（上游同名文件），会被 `git checkout upstream/main -- i18n/index.test.ts`
// 一起抹掉，守卫与被守卫者同沉。隔离到这个 fork-only 文件后，feature 键再被 merge 丢掉，
// 本测试立刻失败报警。
//
// 每次 merge i18n 管线后：`npm --prefix ./web run test -- --run src/i18n/test/forkKeys.test.ts`。

const LOCALES = ['en', 'zh', 'zh-TW'] as const;

// fork-unique 且前端实际引用的键（plain，无复数后缀）。
const FORK_PLAIN_KEYS = [
  'usage_stats.model_filter',
  'usage_stats.all_models',
  'usage_stats.api_key_summary_title',
  'usage_stats.api_key',
  'usage_stats.credentials_refresh_single',
  'common.clear_cache',
  'common.clear_cache_confirm',
];

// 复数后缀键（i18next _one/_other 机制）。
const FORK_PLURAL_KEYS = ['usage_stats.model_filter_selected'];

describe('fork-unique i18n keys exist in every locale', () => {
  for (const key of FORK_PLAIN_KEYS) {
    it(`${key} defined in all locales`, () => {
      for (const lng of LOCALES) {
        const value = i18n.getResource(lng, 'translation', key);
        expect(value, `${lng} missing ${key}`).toBeTruthy();
        expect(typeof value, `${lng} ${key} should be string`).toBe('string');
      }
    });
  }

  for (const key of FORK_PLURAL_KEYS) {
    it(`${key} has _one/_other plural forms in all locales`, () => {
      for (const lng of LOCALES) {
        expect(i18n.getResource(lng, 'translation', `${key}_one`), `${lng} missing ${key}_one`).toBeTruthy();
        expect(i18n.getResource(lng, 'translation', `${key}_other`), `${lng} missing ${key}_other`).toBeTruthy();
      }
    });
  }

  it('usage_stats.model_filter 用对简繁（防 zh/zh-TW 错位）', () => {
    expect(i18n.getResource('zh', 'translation', 'usage_stats.model_filter')).toBe('模型筛选');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.model_filter')).toBe('模型篩選');
  });

  it('usage_stats.api_key_summary_title 用对简繁', () => {
    expect(i18n.getResource('zh', 'translation', 'usage_stats.api_key_summary_title')).toBe('API Key 汇总');
    expect(i18n.getResource('zh-TW', 'translation', 'usage_stats.api_key_summary_title')).toBe('API Key 匯總');
  });

  it('common.clear_cache_confirm 用对简繁（zh 刷新 / zh-TW 重新整理）', () => {
    expect(i18n.getResource('zh', 'translation', 'common.clear_cache_confirm')).toBe('清空所有偏好并刷新？');
    expect(i18n.getResource('zh-TW', 'translation', 'common.clear_cache_confirm')).toBe('清空所有偏好並重新整理？');
  });
});
