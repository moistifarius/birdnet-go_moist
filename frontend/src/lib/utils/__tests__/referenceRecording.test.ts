import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { ReferenceResult } from '$lib/types/detection.types';

const getMock = vi.fn();

vi.mock('$lib/utils/api', () => ({
  api: {
    get: (...args: unknown[]) => getMock(...args),
  },
}));

vi.mock('$lib/utils/logger', () => ({
  loggers: {
    ui: { error: vi.fn(), warn: vi.fn(), info: vi.fn(), debug: vi.fn() },
  },
}));

import {
  fetchReference,
  getCachedReference,
  clearReferenceCache,
  referenceSourceLabel,
} from '../referenceRecording';

describe('fetchReference', () => {
  beforeEach(() => {
    clearReferenceCache();
    getMock.mockReset();
  });

  it('fetches, returns and caches a result', async () => {
    const result: ReferenceResult = {
      enabled: true,
      best: { id: '1', audioUrl: 'https://example.test/1.mp3', sourceProvider: 'xeno-canto' },
    };
    getMock.mockResolvedValue(result);

    const first = await fetchReference(1);
    expect(first).toEqual(result);
    expect(getMock).toHaveBeenCalledWith('/api/v2/detections/1/reference');
    expect(getCachedReference(1)).toEqual(result);

    const second = await fetchReference(1);
    expect(second).toEqual(result);
    expect(getMock).toHaveBeenCalledTimes(1);
  });

  it('coalesces concurrent requests', async () => {
    getMock.mockResolvedValue({ enabled: true });
    const [a, b] = await Promise.all([fetchReference(2), fetchReference(2)]);
    expect(a).toEqual(b);
    expect(getMock).toHaveBeenCalledTimes(1);
  });

  it('resolves to null on error without caching', async () => {
    getMock.mockRejectedValueOnce(new Error('boom'));
    const failed = await fetchReference(3);
    expect(failed).toBeNull();
    expect(getCachedReference(3)).toBeUndefined();
  });
});

describe('referenceSourceLabel', () => {
  it('maps known providers and passes through others', () => {
    expect(referenceSourceLabel('xeno-canto')).toBe('Xeno-canto');
    expect(referenceSourceLabel('other')).toBe('other');
    expect(referenceSourceLabel(undefined)).toBe('');
  });
});
