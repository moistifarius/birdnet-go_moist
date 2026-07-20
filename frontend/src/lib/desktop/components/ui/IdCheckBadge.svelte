<!--
  IdCheckBadge.svelte

  A compact "identification check" verdict pill (Strong / Mixed / Weak support)
  for detection cards. It lazily fetches the verdict only when the card is
  visible, caches per detection, and renders nothing when the feature is
  disabled or the fetch fails — so it never disturbs the surrounding card.

  Uses words + an icon (not color alone) to convey the verdict.
-->
<script lang="ts">
  import type { IdCheckResult, IdCheckVerdict } from '$lib/types/detection.types';
  import StatusPill from './StatusPill.svelte';
  import type { StatusSize } from './StatusPill.svelte';
  import { fetchIdCheck, getCachedIdCheck } from '$lib/utils/idCheck';
  import { verdictVariant } from './idCheckPresentation';
  import { t } from '$lib/i18n';
  import { CircleCheck, CircleAlert, TriangleAlert } from '@lucide/svelte';

  interface Props {
    detectionId: number;
    /** Only fetch/show once the card is visible (mirrors the spectrogram loader). */
    visible?: boolean;
    size?: StatusSize;
    className?: string;
  }

  let { detectionId, visible = true, size = 'sm', className = '' }: Props = $props();

  let result = $state<IdCheckResult | null>(null);

  $effect(() => {
    const id = detectionId;
    if (!visible) return;

    const cached = getCachedIdCheck(id);
    if (cached !== undefined) {
      result = cached;
      return;
    }

    let cancelled = false;
    void fetchIdCheck(id).then(r => {
      if (!cancelled) result = r;
    });
    return () => {
      cancelled = true;
    };
  });

  let verdict = $derived<IdCheckVerdict | undefined>(result?.enabled ? result.verdict : undefined);
</script>

{#if verdict}
  <span title={t('idCheck.title')}>
    <StatusPill
      variant={verdictVariant(verdict)}
      label={t(`idCheck.badge.${verdict}`)}
      {size}
      {className}
    >
      {#snippet leadingIcon()}
        {#if verdict === 'strong'}
          <CircleCheck class="size-3.5" />
        {:else if verdict === 'mixed'}
          <CircleAlert class="size-3.5" />
        {:else}
          <TriangleAlert class="size-3.5" />
        {/if}
      {/snippet}
    </StatusPill>
  </span>
{/if}
