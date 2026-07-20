/**
 * Identification-check ("ID check") fetch helper.
 *
 * The verdict is a lightweight, read-only decision aid served on demand by
 * `GET /api/v2/detections/:id/id-check`. Cards fetch it lazily only when they
 * become visible, so results are cached per detection id for the session and
 * concurrent requests for the same id are de-duplicated. A failed fetch (or an
 * offline online-source) resolves to `null` so callers can simply render
 * nothing — it must never break the surrounding detection UI.
 */
import { api } from './api';
import type { IdCheckResult } from '$lib/types/detection.types';
import { loggers } from './logger';

const logger = loggers.ui;

const cache = new Map<number, IdCheckResult>();
const inFlight = new Map<number, Promise<IdCheckResult | null>>();

/** Returns a previously fetched result for a detection, if any. */
export function getCachedIdCheck(id: number): IdCheckResult | undefined {
  return cache.get(id);
}

/**
 * Fetches the identification-check result for a detection, caching it for the
 * session and coalescing concurrent requests. Resolves to `null` on any error
 * so the caller can render nothing without a broken UI state.
 */
export async function fetchIdCheck(id: number): Promise<IdCheckResult | null> {
  const cached = cache.get(id);
  if (cached !== undefined) return cached;

  const existing = inFlight.get(id);
  if (existing !== undefined) return existing;

  const request = (async (): Promise<IdCheckResult | null> => {
    try {
      const result = await api.get<IdCheckResult>(`/api/v2/detections/${id}/id-check`);
      cache.set(id, result);
      return result;
    } catch (error) {
      logger.error('Failed to fetch identification check', error, { detectionId: id });
      return null;
    } finally {
      inFlight.delete(id);
    }
  })();

  inFlight.set(id, request);
  return request;
}

/** Clears the cache; call after a mutation that could change a verdict. */
export function clearIdCheckCache(): void {
  cache.clear();
  inFlight.clear();
}
