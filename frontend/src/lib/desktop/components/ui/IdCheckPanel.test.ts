import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/svelte';
import IdCheckPanel from './IdCheckPanel.svelte';

const fetchIdCheckMock = vi.fn();

vi.mock('$lib/utils/idCheck', () => ({
  fetchIdCheck: (...args: unknown[]) => fetchIdCheckMock(...args),
  getCachedIdCheck: vi.fn(() => undefined),
}));

describe('IdCheckPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders the verdict, signal reasons and expert details', async () => {
    fetchIdCheckMock.mockResolvedValue({
      enabled: true,
      verdict: 'mixed',
      signals: [
        { code: 'sound_clarity', status: 'pass' },
        { code: 'expected_here', status: 'warn' },
        { code: 'heard_often', status: 'neutral' },
      ],
      details: {
        confidence: 0.9,
        locationConfigured: true,
        occurrence: 0.4,
        dailyCount: 3,
        flaggedUnlikely: false,
        modelType: 'bird',
      },
    });

    render(IdCheckPanel, { props: { detectionId: 1 } });

    // i18n mock returns the key; assert on the keys the panel renders.
    await waitFor(() =>
      expect(screen.getByText('idCheck.verdict.mixed.title')).toBeInTheDocument()
    );
    expect(screen.getByText('idCheck.signal.sound_clarity.pass')).toBeInTheDocument();
    expect(screen.getByText('idCheck.signal.expected_here.warn')).toBeInTheDocument();
    expect(screen.getByText('idCheck.signal.heard_often.neutral')).toBeInTheDocument();
    // Expert details: confidence rendered as a percentage.
    expect(screen.getByText('90%')).toBeInTheDocument();
  });

  it('shows the review note for a weak verdict', async () => {
    fetchIdCheckMock.mockResolvedValue({
      enabled: true,
      verdict: 'weak',
      signals: [{ code: 'sound_clarity', status: 'warn' }],
      details: {
        confidence: 0.3,
        locationConfigured: false,
        dailyCount: 1,
        flaggedUnlikely: false,
      },
    });

    render(IdCheckPanel, { props: { detectionId: 2 } });
    await waitFor(() => expect(screen.getByText('idCheck.verdict.weak.note')).toBeInTheDocument());
    // Occurrence omitted -> "not checked" label.
    expect(screen.getByText('idCheck.details.notChecked')).toBeInTheDocument();
  });

  it('renders nothing when the feature is disabled', async () => {
    fetchIdCheckMock.mockResolvedValue({ enabled: false });
    const { container } = render(IdCheckPanel, { props: { detectionId: 3 } });
    await waitFor(() => expect(fetchIdCheckMock).toHaveBeenCalled());
    await waitFor(() => expect(container.querySelector('.id-check-panel')).toBeNull());
    expect(container.textContent).not.toContain('idCheck.verdict');
  });
});
