/**
 * Reference-recording ("trusted example") fetch helper.
 *
 * Fetches the closest trusted example(s) for a detection's species from
 * `GET /api/v2/detections/:id/reference`, caching per detection for the session
 * and de-duplicating concurrent requests. A failed fetch resolves to `null` so
 * the caller can simply render nothing — an online-source problem must never
 * disturb the rest of the detection UI.
 */
import { api } from './api';
import type { ReferenceResult } from '$lib/types/detection.types';
import { loggers } from './logger';

const logger = loggers.ui;

const cache = new Map<number, ReferenceResult>();
const inFlight = new Map<number, Promise<ReferenceResult | null>>();

/** Returns a previously fetched reference result for a detection, if any. */
export function getCachedReference(id: number): ReferenceResult | undefined {
  return cache.get(id);
}

/**
 * Fetches the reference result for a detection, caching it for the session and
 * coalescing concurrent requests. Resolves to `null` on any error.
 */
export async function fetchReference(id: number): Promise<ReferenceResult | null> {
  const cached = cache.get(id);
  if (cached !== undefined) return cached;

  const existing = inFlight.get(id);
  if (existing !== undefined) return existing;

  const request = (async (): Promise<ReferenceResult | null> => {
    try {
      const result = await api.get<ReferenceResult>(`/api/v2/detections/${id}/reference`);
      cache.set(id, result);
      return result;
    } catch (error) {
      logger.error('Failed to fetch reference recording', error, { detectionId: id });
      return null;
    } finally {
      inFlight.delete(id);
    }
  })();

  inFlight.set(id, request);
  return request;
}

/** Clears the reference cache. */
export function clearReferenceCache(): void {
  cache.clear();
  inFlight.clear();
}

/** Maps a source-provider id to a human-facing label. */
export function referenceSourceLabel(provider: string | undefined): string {
  return provider === 'xeno-canto' ? 'Xeno-canto' : (provider ?? '');
}
