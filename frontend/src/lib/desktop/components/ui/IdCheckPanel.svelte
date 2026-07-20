<!--
  IdCheckPanel.svelte

  The full "identification check" panel for the detection detail view. It shows a
  plain-language verdict (Strong / Mixed / Weak support), a short reason for each
  signal (using words + icons, not color alone), and a collapsed "Details for
  experts" section with the raw numbers.

  It is a read-only decision aid distinct from the human review status, and it
  degrades quietly: while the feature is disabled or the fetch fails, nothing is
  shown so the rest of the detail view is unaffected.
-->
<script lang="ts">
  import type {
    IdCheckResult,
    IdCheckSignalStatus,
    IdCheckVerdict,
  } from '$lib/types/detection.types';
  import { fetchIdCheck } from '$lib/utils/idCheck';
  import { verdictVariant, signalVariant } from './idCheckPresentation';
  import { t } from '$lib/i18n';
  import { cn } from '$lib/utils/cn';
  import {
    CircleCheck,
    CircleAlert,
    TriangleAlert,
    CircleHelp,
    Minus,
    ChevronDown,
  } from '@lucide/svelte';

  interface Props {
    detectionId: number;
    className?: string;
  }

  let { detectionId, className = '' }: Props = $props();

  let result = $state<IdCheckResult | null>(null);
  let loading = $state(true);

  $effect(() => {
    const id = detectionId;
    loading = true;
    result = null;
    let cancelled = false;
    void fetchIdCheck(id).then(r => {
      if (!cancelled) {
        result = r;
        loading = false;
      }
    });
    return () => {
      cancelled = true;
    };
  });

  let verdict = $derived<IdCheckVerdict | undefined>(result?.enabled ? result.verdict : undefined);
  let signals = $derived(result?.signals ?? []);
  let details = $derived(result?.details);

  // CSS variable per semantic variant, for coloring the small signal icons.
  const variantColorVar: Record<string, string> = {
    success: '--color-success',
    warning: '--color-warning',
    error: '--color-error',
    info: '--color-info',
    neutral: '--color-base-content',
  };

  function iconColor(status: IdCheckSignalStatus): string {
    return `color: var(${variantColorVar[signalVariant(status)] ?? '--color-base-content'});`;
  }

  function occurrencePercent(occurrence: number | undefined): string {
    if (occurrence === undefined) return t('idCheck.details.notChecked');
    return `${Math.round(occurrence * 100)}%`;
  }
</script>

{#if loading}
  <div
    class="flex items-center gap-2 text-sm text-base-content/60 py-2"
    role="status"
    aria-live="polite"
  >
    <span class="loading loading-spinner loading-xs" aria-hidden="true"></span>
    <span>{t('idCheck.loading')}</span>
  </div>
{:else if verdict}
  <section class={cn('id-check-panel', className)} aria-label={t('idCheck.title')}>
    <!-- Verdict header -->
    <div class="flex items-center gap-2">
      <span
        style={`color: var(${variantColorVar[verdictVariant(verdict)] ?? '--color-base-content'});`}
      >
        {#if verdict === 'strong'}
          <CircleCheck class="size-5" />
        {:else if verdict === 'mixed'}
          <CircleAlert class="size-5" />
        {:else}
          <TriangleAlert class="size-5" />
        {/if}
      </span>
      <h3 class="text-base font-semibold">{t(`idCheck.verdict.${verdict}.title`)}</h3>
    </div>

    {#if verdict === 'weak'}
      <p class="mt-1 text-sm text-base-content/70">{t('idCheck.verdict.weak.note')}</p>
    {/if}

    <!-- Plain-language signals -->
    <ul class="mt-3 space-y-2">
      {#each signals as signal (signal.code)}
        <li class="flex items-start gap-2 text-sm">
          <span class="mt-0.5 flex-shrink-0" style={iconColor(signal.status)} aria-hidden="true">
            {#if signal.status === 'pass'}
              <CircleCheck class="size-4" />
            {:else if signal.status === 'warn'}
              <TriangleAlert class="size-4" />
            {:else if signal.status === 'neutral'}
              <Minus class="size-4" />
            {:else}
              <CircleHelp class="size-4" />
            {/if}
          </span>
          <span>{t(`idCheck.signal.${signal.code}.${signal.status}`)}</span>
        </li>
      {/each}
    </ul>

    <!-- Details for experts -->
    {#if details}
      <details class="mt-3 group">
        <summary
          class="flex cursor-pointer items-center gap-1 text-sm text-base-content/70 hover:text-base-content"
        >
          <ChevronDown class="size-4 transition-transform group-open:rotate-180" />
          {t('idCheck.expertDetails')}
        </summary>
        <dl class="mt-2 grid grid-cols-2 gap-x-4 gap-y-1 text-sm">
          <dt class="text-base-content/60">{t('idCheck.details.confidence')}</dt>
          <dd>{Math.round(details.confidence * 100)}%</dd>
          <dt class="text-base-content/60">{t('idCheck.details.expectedness')}</dt>
          <dd>{occurrencePercent(details.occurrence)}</dd>
          <dt class="text-base-content/60">{t('idCheck.details.timesHeard')}</dt>
          <dd>{details.dailyCount}</dd>
          {#if details.modelType}
            <dt class="text-base-content/60">{t('idCheck.details.model')}</dt>
            <dd>{details.modelType}</dd>
          {/if}
        </dl>
      </details>
    {/if}
  </section>
{/if}
