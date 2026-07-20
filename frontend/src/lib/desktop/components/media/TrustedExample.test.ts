import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/svelte';
import TrustedExample from './TrustedExample.svelte';
import type { ReferenceResult } from '$lib/types/detection.types';

const fetchReferenceMock = vi.fn();

vi.mock('$lib/utils/referenceRecording', async importOriginal => {
  const actual = await importOriginal<typeof import('$lib/utils/referenceRecording')>();
  return {
    ...actual,
    fetchReference: (...args: unknown[]) => fetchReferenceMock(...args),
    getCachedReference: vi.fn(() => undefined),
  };
});

function audioSrc(container: HTMLElement): string | null {
  return container.querySelector('audio')?.getAttribute('src') ?? null;
}

const twoExamples: ReferenceResult = {
  enabled: true,
  best: {
    id: '1',
    audioUrl: 'https://example.test/1.mp3',
    recordist: 'Jane Smith',
    sourceProvider: 'xeno-canto',
    pageUrl: 'https://example.test/page/1',
    licenseName: 'CC BY 4.0',
    licenseUrl: 'https://creativecommons.org/licenses/by/4.0/',
  },
  alternatives: [{ id: '2', audioUrl: 'https://example.test/2.mp3', sourceProvider: 'xeno-canto' }],
};

describe('TrustedExample', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders the example audio and attribution when enabled', async () => {
    fetchReferenceMock.mockResolvedValue(twoExamples);
    const { container } = render(TrustedExample, { props: { detectionId: 1 } });

    await waitFor(() => expect(screen.getByText('reference.title')).toBeInTheDocument());
    expect(audioSrc(container)).toBe('https://example.test/1.mp3');
    // i18n mock returns the key; attribution key is rendered when a recordist exists.
    expect(screen.getByText('reference.attribution')).toBeInTheDocument();
    expect(screen.getByText('CC BY 4.0')).toBeInTheDocument();
  });

  it('cycles to another example with "try another"', async () => {
    fetchReferenceMock.mockResolvedValue(twoExamples);
    const { container } = render(TrustedExample, { props: { detectionId: 1 } });

    await waitFor(() => expect(audioSrc(container)).toBe('https://example.test/1.mp3'));
    await fireEvent.click(screen.getByText('reference.tryAnother'));
    await waitFor(() => expect(audioSrc(container)).toBe('https://example.test/2.mp3'));
  });

  it('renders nothing when disabled', async () => {
    fetchReferenceMock.mockResolvedValue({ enabled: false });
    const { container } = render(TrustedExample, { props: { detectionId: 2 } });

    await waitFor(() => expect(fetchReferenceMock).toHaveBeenCalled());
    await waitFor(() => expect(container.querySelector('audio')).toBeNull());
    expect(container.textContent).not.toContain('reference.title');
  });

  it('hides "try another" when there is a single example', async () => {
    fetchReferenceMock.mockResolvedValue({
      enabled: true,
      best: { id: '1', audioUrl: 'https://example.test/1.mp3', sourceProvider: 'xeno-canto' },
    });
    render(TrustedExample, { props: { detectionId: 3 } });

    await waitFor(() => expect(screen.getByText('reference.title')).toBeInTheDocument());
    expect(screen.queryByText('reference.tryAnother')).toBeNull();
  });
});
