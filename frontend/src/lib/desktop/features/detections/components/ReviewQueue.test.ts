import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/svelte';
import ReviewQueue from './ReviewQueue.svelte';

const postMock = vi.fn();
const setVerificationMock = vi.fn().mockResolvedValue(true);

vi.mock('$lib/utils/api', () => ({
  api: { post: (...args: unknown[]) => postMock(...args) },
}));

vi.mock('$lib/utils/reviewDetection', () => ({
  setDetectionVerification: (...args: unknown[]) => setVerificationMock(...args),
}));

function item(id: string, commonName: string) {
  return {
    id,
    commonName,
    scientificName: 'Sci ' + commonName,
    confidence: 0.5,
    verified: 'unverified',
    hasAudio: false,
    timestamp: '2025-07-01T08:00:00Z',
    timeOfDay: 'day',
  };
}

describe('ReviewQueue', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setVerificationMock.mockResolvedValue(true);
    postMock.mockResolvedValue({ results: [item('10', 'Robin'), item('11', 'Sparrow')], total: 2 });
  });

  it('shows the banner and opens the queue', async () => {
    render(ReviewQueue, { props: {} });
    await waitFor(() => expect(screen.getByText('reviewQueue.banner')).toBeInTheDocument());

    await fireEvent.click(screen.getByText('reviewQueue.review'));
    await waitFor(() => expect(screen.getByText('Robin')).toBeInTheDocument());
  });

  it('persists Correct and advances to the next, then reaches all-done', async () => {
    render(ReviewQueue, { props: {} });
    await waitFor(() => expect(screen.getByText('reviewQueue.review')).toBeInTheDocument());
    await fireEvent.click(screen.getByText('reviewQueue.review'));

    await waitFor(() => expect(screen.getByText('Robin')).toBeInTheDocument());
    await fireEvent.click(screen.getByText('reviewQueue.correct'));
    expect(setVerificationMock).toHaveBeenCalledWith(10, 'correct');

    await waitFor(() => expect(screen.getByText('Sparrow')).toBeInTheDocument());
    await fireEvent.click(screen.getByText('reviewQueue.correct'));
    expect(setVerificationMock).toHaveBeenCalledWith(11, 'correct');

    await waitFor(() => expect(screen.getByText('reviewQueue.allDone')).toBeInTheDocument());
  });

  it('advances on "Not sure" without persisting', async () => {
    render(ReviewQueue, { props: {} });
    await waitFor(() => expect(screen.getByText('reviewQueue.review')).toBeInTheDocument());
    await fireEvent.click(screen.getByText('reviewQueue.review'));

    await waitFor(() => expect(screen.getByText('Robin')).toBeInTheDocument());
    await fireEvent.click(screen.getByText('reviewQueue.notSure'));

    await waitFor(() => expect(screen.getByText('Sparrow')).toBeInTheDocument());
    expect(setVerificationMock).not.toHaveBeenCalled();
  });

  it('shows no banner when nothing needs review', async () => {
    postMock.mockResolvedValue({ results: [], total: 0 });
    render(ReviewQueue, { props: {} });
    await waitFor(() => expect(postMock).toHaveBeenCalled());
    expect(screen.queryByText('reviewQueue.review')).toBeNull();
  });
});
