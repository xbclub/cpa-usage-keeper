/**
 * 集中管理应用所有 localStorage key，供定向清除使用。
 * 新增 key 时请同步登记到 APP_STORAGE_KEYS。
 */
export const APP_STORAGE_KEYS = [
  'cpa-usage-theme',
  'cpa-usage-keeper-language',
  'cpa-usage-keeper-auth-files-active-only',
  'cpa.credentials.authFiles.displayMode',
  'cli-proxy-usage-time-range-v1',
  'cli-proxy-usage-custom-range-v1',
  'cli-proxy-usage-overview-realtime-window-v1',
  'cli-proxy-usage-request-events-preferences-v1',
  'cli-proxy-usage-tab-v1',
  'cli-proxy-key-overview-range-v1',
] as const;

/**
 * 只清除本应用写入的 localStorage key，不影响同源下其他应用的数据。
 */
export function clearAppStorage(): void {
  if (typeof localStorage === 'undefined') return;
  for (const key of APP_STORAGE_KEYS) {
    try {
      localStorage.removeItem(key);
    } catch {
      // 忽略个别 key 的访问异常（如隐私模式/被禁用的 key）。
    }
  }
}
