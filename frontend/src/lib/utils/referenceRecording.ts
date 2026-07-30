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
import type { AlternativesResult, ReferenceResult } from '$lib/types/detection.types';
import { loggers } from './logger';
import { buildAppUrl } from './urlHelpers';

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

const altCache = new Map<number, AlternativesResult>();
const altInFlight = new Map<number, Promise<AlternativesResult | null>>();

/**
 * Fetches the plausible alternative species for a detection (with trusted
 * examples when available), cached per detection. Resolves to `null` on error.
 */
export async function fetchAlternatives(id: number): Promise<AlternativesResult | null> {
  const cached = altCache.get(id);
  if (cached !== undefined) return cached;

  const existing = altInFlight.get(id);
  if (existing !== undefined) return existing;

  const request = (async (): Promise<AlternativesResult | null> => {
    try {
      const result = await api.get<AlternativesResult>(`/api/v2/detections/${id}/alternatives`);
      altCache.set(id, result);
      return result;
    } catch (error) {
      logger.error('Failed to fetch alternative species', error, { detectionId: id });
      return null;
    } finally {
      altInFlight.delete(id);
    }
  })();

  altInFlight.set(id, request);
  return request;
}

/** Maps a source-provider id to a human-facing label. */
export function referenceSourceLabel(provider: string | undefined): string {
  return provider === 'xeno-canto' ? 'Xeno-canto' : (provider ?? '');
}

/**
 * URL of the server-side cropped + loudness-normalized trusted example for a
 * detection's *detected* species, so the "Compare sounds" screen plays a short
 * example at a consistent, audible level. The endpoint derives the source
 * recording server-side (never trusting a client URL) and is best-effort: it
 * returns a non-2xx status when it cannot produce a processed clip (feature off,
 * no ffmpeg, source unavailable), so callers MUST fall back to the raw catalog
 * `audioUrl` on an audio error. Because the endpoint only knows the detection's
 * species, it is not used for alternative-species examples.
 */
export function processedReferenceClipUrl(detectionId: number, recordingId?: string): string {
  const base = `/api/v2/detections/${detectionId}/reference/clip`;
  const path = recordingId ? `${base}?rec=${encodeURIComponent(recordingId)}` : base;
  return buildAppUrl(path);
}
