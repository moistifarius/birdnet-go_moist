import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { IdCheckResult } from '$lib/types/detection.types';

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

import { fetchIdCheck, getCachedIdCheck, clearIdCheckCache } from '../idCheck';

describe('fetchIdCheck', () => {
  beforeEach(() => {
    clearIdCheckCache();
    getMock.mockReset();
  });

  it('fetches, returns and caches a result', async () => {
    const result: IdCheckResult = { enabled: true, verdict: 'strong', signals: [] };
    getMock.mockResolvedValue(result);

    const first = await fetchIdCheck(1);
    expect(first).toEqual(result);
    expect(getMock).toHaveBeenCalledWith('/api/v2/detections/1/id-check');
    expect(getMock).toHaveBeenCalledTimes(1);
    expect(getCachedIdCheck(1)).toEqual(result);

    const second = await fetchIdCheck(1);
    expect(second).toEqual(result);
    expect(getMock).toHaveBeenCalledTimes(1); // served from cache, no refetch
  });

  it('coalesces concurrent requests for the same id', async () => {
    getMock.mockResolvedValue({ enabled: true, verdict: 'mixed' });
    const [a, b] = await Promise.all([fetchIdCheck(2), fetchIdCheck(2)]);
    expect(a).toEqual(b);
    expect(getMock).toHaveBeenCalledTimes(1);
  });

  it('resolves to null on error without caching, and retries next call', async () => {
    getMock.mockRejectedValueOnce(new Error('boom'));
    const failed = await fetchIdCheck(3);
    expect(failed).toBeNull();
    expect(getCachedIdCheck(3)).toBeUndefined();

    getMock.mockResolvedValueOnce({ enabled: false });
    const retried = await fetchIdCheck(3);
    expect(retried).toEqual({ enabled: false });
    expect(getMock).toHaveBeenCalledTimes(2);
  });

  it('clearIdCheckCache empties the cache', async () => {
    getMock.mockResolvedValue({ enabled: true, verdict: 'weak' });
    await fetchIdCheck(4);
    expect(getCachedIdCheck(4)).toBeDefined();
    clearIdCheckCache();
    expect(getCachedIdCheck(4)).toBeUndefined();
  });
});
