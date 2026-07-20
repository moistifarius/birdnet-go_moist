import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/svelte';
import IdCheckBadge from './IdCheckBadge.svelte';

const fetchIdCheckMock = vi.fn();
const getCachedIdCheckMock = vi.fn();

vi.mock('$lib/utils/idCheck', () => ({
  fetchIdCheck: (...args: unknown[]) => fetchIdCheckMock(...args),
  getCachedIdCheck: (...args: unknown[]) => getCachedIdCheckMock(...args),
}));

describe('IdCheckBadge', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getCachedIdCheckMock.mockReturnValue(undefined);
  });

  it('renders the verdict label when enabled', async () => {
    fetchIdCheckMock.mockResolvedValue({ enabled: true, verdict: 'strong', signals: [] });
    render(IdCheckBadge, { props: { detectionId: 1 } });
    // The i18n mock returns the key; StatusPill renders it as the label.
    await waitFor(() => expect(screen.getByText('idCheck.badge.strong')).toBeInTheDocument());
  });

  it('renders nothing when the feature is disabled', async () => {
    fetchIdCheckMock.mockResolvedValue({ enabled: false });
    const { container } = render(IdCheckBadge, { props: { detectionId: 2 } });
    await waitFor(() => expect(fetchIdCheckMock).toHaveBeenCalled());
    expect(container.textContent).not.toContain('idCheck.badge');
  });

  it('renders nothing when the fetch fails', async () => {
    fetchIdCheckMock.mockResolvedValue(null);
    const { container } = render(IdCheckBadge, { props: { detectionId: 5 } });
    await waitFor(() => expect(fetchIdCheckMock).toHaveBeenCalled());
    expect(container.textContent).not.toContain('idCheck.badge');
  });

  it('does not fetch while the card is not visible', () => {
    render(IdCheckBadge, { props: { detectionId: 3, visible: false } });
    expect(fetchIdCheckMock).not.toHaveBeenCalled();
  });

  it('uses a cached result without fetching', async () => {
    getCachedIdCheckMock.mockReturnValue({ enabled: true, verdict: 'weak', signals: [] });
    render(IdCheckBadge, { props: { detectionId: 4 } });
    await waitFor(() => expect(screen.getByText('idCheck.badge.weak')).toBeInTheDocument());
    expect(fetchIdCheckMock).not.toHaveBeenCalled();
  });
});
