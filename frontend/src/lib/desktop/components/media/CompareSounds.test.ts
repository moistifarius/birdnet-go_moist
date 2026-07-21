import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/svelte';
import CompareSounds from './CompareSounds.svelte';
import type { Detection } from '$lib/types/detection.types';

const fetchReferenceMock = vi.fn();
const setVerificationMock = vi.fn().mockResolvedValue(true);

vi.mock('$lib/utils/referenceRecording', async importOriginal => {
  const actual = await importOriginal<typeof import('$lib/utils/referenceRecording')>();
  return { ...actual, fetchReference: (...args: unknown[]) => fetchReferenceMock(...args) };
});

vi.mock('$lib/utils/reviewDetection', () => ({
  setDetectionVerification: (...args: unknown[]) => setVerificationMock(...args),
}));

vi.mock('$lib/utils/spectrogramLoader.svelte', () => ({
  createSpectrogramLoader: () => ({
    start: vi.fn(),
    stop: vi.fn(),
    destroy: vi.fn(),
    handleImageLoad: vi.fn(),
    handleImageError: vi.fn(),
    get spectrogramUrl() {
      return 'blob:local-spectrogram';
    },
    get showSpinner() {
      return false;
    },
    get error() {
      return null;
    },
    get state() {
      return 'loaded';
    },
    get isQueued() {
      return false;
    },
    get isGenerating() {
      return false;
    },
  }),
}));

function detection(): Detection {
  return {
    id: 42,
    date: '2025-07-01',
    time: '08:00:00',
    beginTime: '',
    endTime: '',
    speciesCode: 'amerob',
    scientificName: 'Turdus migratorius',
    commonName: 'American Robin',
    confidence: 0.9,
    verified: 'unverified',
    locked: false,
  };
}

const referenceResult = {
  enabled: true,
  best: {
    id: '1',
    audioUrl: 'https://example.test/1.mp3',
    sonogramUrl: 'https://example.test/sono.png',
    recordist: 'Jane Smith',
    sourceProvider: 'xeno-canto',
  },
};

describe('CompareSounds', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setVerificationMock.mockResolvedValue(true);
  });

  it('shows the two sound pictures and keeps the ID on "correct"', async () => {
    fetchReferenceMock.mockResolvedValue(referenceResult);
    const onClose = vi.fn();
    const onReviewed = vi.fn();
    render(CompareSounds, {
      props: { detection: detection(), isOpen: true, onClose, onReviewed },
    });

    await waitFor(() =>
      expect(screen.getByText('reference.compare.yourRecording')).toBeInTheDocument()
    );
    expect(screen.getByText('reference.compare.auto')).toBeInTheDocument();

    await fireEvent.click(screen.getByText('reference.compare.keep'));
    await waitFor(() => expect(setVerificationMock).toHaveBeenCalledWith(42, 'correct'));
    expect(onReviewed).toHaveBeenCalledWith('correct');
    expect(onClose).toHaveBeenCalled();
  });

  it('marks false positive after choosing a "what seems wrong" reason', async () => {
    fetchReferenceMock.mockResolvedValue(referenceResult);
    render(CompareSounds, {
      props: { detection: detection(), isOpen: true, onClose: vi.fn() },
    });

    await waitFor(() => expect(screen.getByText('reference.compare.wrong')).toBeInTheDocument());
    await fireEvent.click(screen.getByText('reference.compare.wrong'));

    await waitFor(() =>
      expect(screen.getByText('reference.compare.wrongReason.title')).toBeInTheDocument()
    );
    await fireEvent.click(screen.getByText('reference.compare.wrongReason.anotherBird'));
    await waitFor(() => expect(setVerificationMock).toHaveBeenCalledWith(42, 'false_positive'));
  });

  it('accepts "Not sure" without persisting anything', async () => {
    fetchReferenceMock.mockResolvedValue(referenceResult);
    render(CompareSounds, {
      props: { detection: detection(), isOpen: true, onClose: vi.fn() },
    });

    await waitFor(() => expect(screen.getByText('reference.compare.notSure')).toBeInTheDocument());
    await fireEvent.click(screen.getByText('reference.compare.notSure'));

    await waitFor(() =>
      expect(screen.getByText('reference.compare.notSureNote')).toBeInTheDocument()
    );
    expect(setVerificationMock).not.toHaveBeenCalled();
  });

  it('shows a friendly message when no example exists', async () => {
    fetchReferenceMock.mockResolvedValue({ enabled: true });
    render(CompareSounds, {
      props: { detection: detection(), isOpen: true, onClose: vi.fn() },
    });

    await waitFor(() =>
      expect(screen.getByText('reference.compare.noExample')).toBeInTheDocument()
    );
  });
});
