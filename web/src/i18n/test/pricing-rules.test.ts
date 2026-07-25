import { describe, expect, it } from 'vitest'
import i18n, { SUPPORTED_LANGUAGES } from '../index'

describe('pricing rule translations', () => {
  it('defines the Rules UI copy in every supported language', () => {
    for (const language of SUPPORTED_LANGUAGES) {
      const usageStats = i18n.getResourceBundle(language, 'translation').usage_stats
      for (const key of [
        'model_price_rules',
        'model_price_rules_title',
        'model_price_rules_key',
        'model_price_rules_value',
        'model_price_rules_multiplier',
        'model_price_rules_add',
        'model_price_rules_remove',
        'model_price_rules_help',
        'model_price_rules_help_examples',
        'model_price_rules_help_service_tier',
        'model_price_rules_help_reasoning_effort',
      ]) {
        expect(usageStats[key], `${language}.${key}`).toBeTruthy()
      }
    }
  })

  it('keeps the help copy limited to the two approved examples', () => {
    for (const language of SUPPORTED_LANGUAGES) {
      const usageStats = i18n.getResourceBundle(language, 'translation').usage_stats
      const help = [
        usageStats.model_price_rules_help,
        usageStats.model_price_rules_help_service_tier,
        usageStats.model_price_rules_help_reasoning_effort,
      ].join(' ')
      expect(help).toContain('service_tier')
      expect(help).toContain('priority')
      expect(help).toContain('reasoning_effort')
      expect(help).toContain('xhigh')
      expect(help).not.toContain('api_group_key')
      expect(help).not.toContain('response_service_tier')
      expect(help).not.toContain('executor_type')
    }
  })

  it('uses a compact remove label in every supported language', () => {
    expect(i18n.getResourceBundle('en', 'translation').usage_stats.model_price_rules_remove).toBe('Remove')
    expect(i18n.getResourceBundle('zh', 'translation').usage_stats.model_price_rules_remove).toBe('删除')
    expect(i18n.getResourceBundle('zh-TW', 'translation').usage_stats.model_price_rules_remove).toBe('刪除')
  })

  it('labels the approved help examples in every supported language', () => {
    expect(i18n.getResourceBundle('en', 'translation').usage_stats.model_price_rules_help_examples).toBe('Examples:')
    expect(i18n.getResourceBundle('zh', 'translation').usage_stats.model_price_rules_help_examples).toBe('示例：')
    expect(i18n.getResourceBundle('zh-TW', 'translation').usage_stats.model_price_rules_help_examples).toBe('範例：')
  })
})
