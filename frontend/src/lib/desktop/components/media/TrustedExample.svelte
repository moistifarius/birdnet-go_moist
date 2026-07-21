<!--
  TrustedExample.svelte

  Plays the closest "trusted example" reference recording for a detection's
  species beside what the microphone heard, with attribution and a "Try another
  example" control that cycles through contextually-ranked alternatives.

  It uses a native <audio> element so cross-origin catalog audio plays without
  CORS, and it degrades quietly: while the feature is disabled, unconfigured, or
  the online source is unavailable, nothing renders and local detection playback
  is unaffected.
-->
<script lang="ts">
  import type { ReferenceRecording, ReferenceResult } from '$lib/types/detection.types';
  import { fetchReference, referenceSourceLabel } from '$lib/utils/referenceRecording';
  import { t } from '$lib/i18n';
  import { cn } from '$lib/utils/cn';
  import { RefreshCw, ExternalLink, GitCompareArrows } from '@lucide/svelte';

  interface Props {
    detectionId: number;
    className?: string;
    /** When provided, shows a "Compare sounds" button that invokes this. */
    onCompare?: () => void;
  }

  let { detectionId, className = '', onCompare }: Props = $props();

  let result = $state<ReferenceResult | null>(null);
  let loading = $state(true);
  let index = $state(0);

  $effect(() => {
    const id = detectionId;
    loading = true;
    result = null;
    index = 0;
    let cancelled = false;
    void fetchReference(id).then(r => {
      if (!cancelled) {
        result = r;
        loading = false;
      }
    });
    return () => {
      cancelled = true;
    };
  });

  let examples = $derived<ReferenceRecording[]>(
    result?.enabled && result.best ? [result.best, ...(result.alternatives ?? [])] : []
  );
  let current = $derived<ReferenceRecording | undefined>(examples.at(index));
  let hasMultiple = $derived(examples.length > 1);
  let source = $derived(referenceSourceLabel(current?.sourceProvider));

  function tryAnother() {
    if (examples.length === 0) return;
    index = (index + 1) % examples.length;
  }
</script>

{#if loading}
  <div
    class="flex items-center gap-2 text-sm text-base-content/60 py-2"
    role="status"
    aria-live="polite"
  >
    <span class="loading loading-spinner loading-xs" aria-hidden="true"></span>
    <span>{t('reference.loading')}</span>
  </div>
{:else if current}
  <section class={cn('trusted-example', className)} aria-label={t('reference.title')}>
    <h3 class="text-base font-semibold">{t('reference.title')}</h3>

    <audio
      class="mt-2 w-full"
      controls
      preload="none"
      src={current.audioUrl}
      aria-label={t('reference.audioAria', { source })}
    ></audio>

    <p class="mt-2 text-sm text-base-content/70">
      {#if current.recordist}
        {t('reference.attribution', { source, recordist: current.recordist })}
      {:else}
        {t('reference.attributionNoRecordist', { source })}
      {/if}
    </p>

    <div class="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-base-content/60">
      {#if current.licenseName}
        {#if current.licenseUrl}
          <a
            href={current.licenseUrl}
            target="_blank"
            rel="noopener noreferrer"
            class="link link-hover"
          >
            {current.licenseName}
          </a>
        {:else}
          <span>{current.licenseName}</span>
        {/if}
      {/if}
      {#if current.pageUrl}
        <a
          href={current.pageUrl}
          target="_blank"
          rel="noopener noreferrer"
          class="link link-hover inline-flex items-center gap-1"
        >
          <ExternalLink class="size-3" />
          {t('reference.viewSource')}
        </a>
      {/if}
    </div>

    <div class="mt-2 flex flex-wrap gap-2">
      {#if hasMultiple}
        <button type="button" class="btn btn-sm btn-ghost gap-1" onclick={tryAnother}>
          <RefreshCw class="size-4" />
          {t('reference.tryAnother')}
        </button>
      {/if}
      {#if onCompare}
        <button type="button" class="btn btn-sm btn-primary gap-1" onclick={onCompare}>
          <GitCompareArrows class="size-4" />
          {t('reference.compare.title')}
        </button>
      {/if}
    </div>
  </section>
{/if}
