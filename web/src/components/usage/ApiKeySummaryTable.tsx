import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import type { UsageOverviewPayload } from './hooks/useUsageData';
import { formatCompactNumber, formatUsd } from '@/utils/usage';
import styles from './ApiKeySummaryTable.module.scss';

interface ApiKeySummaryTableProps {
  usage: UsageOverviewPayload | null;
  loading: boolean;
}

export function ApiKeySummaryTable({ usage, loading }: ApiKeySummaryTableProps) {
  const { t } = useTranslation();
  const summary = usage?.api_key_summary;

  const rows = useMemo(() => {
    if (!summary || summary.length === 0) return [];
    return summary;
  }, [summary]);

  if (loading && !usage) return null;
  if (rows.length === 0) return null;

  return (
    <div className={styles.container}>
      <h3 className={styles.title}>{t('usage_stats.api_key_summary_title')}</h3>
      <div className={styles.tableWrapper}>
        <table className={styles.table}>
          <thead>
            <tr>
              <th>{t('usage_stats.api_key')}</th>
              <th>{t('usage_stats.total_requests')}</th>
              <th>{t('usage_stats.total_tokens')}</th>
              <th>{t('usage_stats.input_tokens')}</th>
              <th>{t('usage_stats.output_tokens')}</th>
              <th>{t('usage_stats.cache_read_tokens')}</th>
              <th>{t('usage_stats.total_cost')}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.api_key}>
                <td className={styles.keyCell} title={row.display_name || row.api_key}>
                  {row.display_name || row.api_key}
                </td>
                <td>{row.request_count.toLocaleString()}</td>
                <td>{formatCompactNumber(row.total_tokens)}</td>
                <td>{formatCompactNumber(row.input_tokens)}</td>
                <td>{formatCompactNumber(row.output_tokens)}</td>
                <td>{formatCompactNumber(row.cache_read_tokens)}</td>
                <td>{row.cost_available ? formatUsd(row.cost_usd) : '-'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
