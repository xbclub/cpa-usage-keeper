import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ApiError, deletePricing, fetchPricing, fetchUsedModels, updatePricing } from '@/lib/api';
import type { ModelPrice, PricingEntry, PricingStyle } from '@/lib/types';
import { useNotificationStore } from '@/stores';

export interface UsePricingDataOptions {
  onAuthRequired?: () => void;
  enabled?: boolean;
}

export interface UsePricingDataReturn {
  modelNames: string[];
  modelPrices: Record<string, ModelPrice>;
  loading: boolean;
  error: string;
  lastRefreshedAt: Date | null;
  loadPricing: () => Promise<void>;
  setModelPrices: (prices: Record<string, ModelPrice>) => Promise<void>;
}

const normalizePricingStyle = (style: PricingStyle | string | undefined): PricingStyle =>
  style === 'claude' ? 'claude' : 'openai';

const pricingToModelPrice = (entry: PricingEntry): ModelPrice => ({
  style: normalizePricingStyle(entry.pricing_style),
  prompt: entry.prompt_price_per_1m,
  completion: entry.completion_price_per_1m,
  cache: entry.cache_price_per_1m,
  cacheCreation: entry.cache_creation_price_per_1m ?? 0,
});

export function usePricingData(options: UsePricingDataOptions = {}): UsePricingDataReturn {
  const { onAuthRequired, enabled = true } = options;
  const { t } = useTranslation();
  const { showNotification } = useNotificationStore();
  const [modelNames, setModelNames] = useState<string[]>([]);
  const [modelPrices, setModelPricesState] = useState<Record<string, ModelPrice>>({});
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [lastRefreshedAt, setLastRefreshedAt] = useState<Date | null>(null);
  const requestControllerRef = useRef<AbortController | null>(null);
  const onAuthRequiredRef = useRef(onAuthRequired);

  useEffect(() => {
    onAuthRequiredRef.current = onAuthRequired;
  }, [onAuthRequired]);

  const applyPricingResponse = useCallback((pricingResponse: Awaited<ReturnType<typeof fetchPricing>>) => {
    const prices = Object.fromEntries(
      pricingResponse.pricing.map((entry) => [entry.model, pricingToModelPrice(entry)])
    );
    setModelPricesState(prices);
    setLastRefreshedAt(new Date());
  }, []);

  const loadPricing = useCallback(async () => {
    requestControllerRef.current?.abort();
    const controller = new AbortController();
    requestControllerRef.current = controller;

    setLoading(true);
    setError('');

    try {
      const [pricingResponse, usedModelsResponse] = await Promise.all([
        fetchPricing(controller.signal),
        fetchUsedModels(controller.signal),
      ]);
      if (requestControllerRef.current !== controller) {
        return;
      }
      applyPricingResponse(pricingResponse);
      setModelNames(usedModelsResponse.models);
    } catch (error) {
      if (controller.signal.aborted) {
        return;
      }
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequiredRef.current?.();
        return;
      }
      setError(error instanceof Error ? error.message : 'Failed to load pricing');
    } finally {
      if (requestControllerRef.current === controller) {
        setLoading(false);
        requestControllerRef.current = null;
      }
    }
  }, [applyPricingResponse]);

  useEffect(() => {
    if (!enabled) {
      requestControllerRef.current?.abort();
      requestControllerRef.current = null;
      setLoading(false);
      return;
    }
    void loadPricing();
    return () => {
      requestControllerRef.current?.abort();
      requestControllerRef.current = null;
    };
  }, [enabled, loadPricing]);

  const setModelPrices = useCallback(async (prices: Record<string, ModelPrice>) => {
    const previousPrices = modelPrices;
    setModelPricesState(prices);

    try {
      const previousModels = new Set(Object.keys(previousPrices));
      const nextModels = new Set(Object.keys(prices));
      await Promise.all([
        ...Object.entries(prices).map(([model, pricing]) =>
          updatePricing(model, {
            prompt_price_per_1m: pricing.prompt,
            completion_price_per_1m: pricing.completion,
            cache_price_per_1m: pricing.cache,
            cache_creation_price_per_1m: pricing.cacheCreation,
            pricing_style: pricing.style,
          })
        ),
        ...Array.from(previousModels)
          .filter((model) => !nextModels.has(model))
          .map((model) => deletePricing(model)),
      ]);
      setLastRefreshedAt(new Date());
    } catch (error) {
      setModelPricesState(previousPrices);
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequiredRef.current?.();
        return;
      }
      const message = error instanceof Error ? error.message : '';
      showNotification(
        `${t('notification.upload_failed')}${message ? `: ${message}` : ''}`,
        'error'
      );
    }
  }, [modelPrices, showNotification, t]);

  return {
    modelNames,
    modelPrices,
    loading,
    error,
    lastRefreshedAt,
    loadPricing,
    setModelPrices,
  };
}
