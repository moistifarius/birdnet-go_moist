/**
 * Shared presentation mapping for identification-check verdicts and signals, so
 * the compact badge and the detailed panel stay visually consistent. Icons are
 * rendered in the components themselves (Lucide components); this module only
 * maps to the semantic StatusPill variant.
 */
import type { StatusVariant } from './StatusPill.svelte';
import type { IdCheckVerdict, IdCheckSignalStatus } from '$lib/types/detection.types';

/** Maps an overall verdict to a semantic color variant. */
export function verdictVariant(verdict: IdCheckVerdict): StatusVariant {
  switch (verdict) {
    case 'strong':
      return 'success';
    case 'mixed':
      return 'warning';
    case 'weak':
      return 'error';
    default:
      return 'neutral';
  }
}

/** Maps a single signal status to a semantic color variant. */
export function signalVariant(status: IdCheckSignalStatus): StatusVariant {
  switch (status) {
    case 'pass':
      return 'success';
    case 'warn':
      return 'warning';
    case 'neutral':
      return 'info';
    case 'unknown':
      return 'neutral';
    default:
      return 'neutral';
  }
}
