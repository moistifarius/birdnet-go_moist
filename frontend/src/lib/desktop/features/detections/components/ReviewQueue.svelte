<!--
  ReviewQueue.svelte

  A one-at-a-time review queue for detections that still need attention
  (unverified). Shows a small banner with the count and, on demand, steps through
  each detection with a quick Wrong / Not sure / Correct decision — the design's
  "review them one at a time" flow. "Not sure" is always acceptable and persists
  nothing.

  It reuses the existing search endpoint (verifiedStatus=unverified) and the
  shared review wrapper, and refreshes the parent list after a session.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '$lib/utils/api';
  import { setDetectionVerification } from '$lib/utils/reviewDetection';
  import SpectrogramPlayer from '$lib/desktop/components/media/SpectrogramPlayer.svelte';
  import Modal from '$lib/desktop/components/ui/Modal.svelte';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { t } from '$lib/i18n';
  import { loggers } from '$lib/utils/logger';
  import { ClipboardList, Check, CircleHelp, X } from '@lucide/svelte';

  interface ReviewQueueItem {
    id: string;
    commonName: string;
    scientificName: string;
    confidence: number;
    verified: string;
    hasAudio: boolean;
    timestamp: string;
    timeOfDay: string;
  }

  interface Props {
    /** Called after a review session so the parent list can refresh. */
    onReviewed?: () => void;
  }

  let { onReviewed }: Props = $props();

  const logger = loggers.ui;

  let items = $state<ReviewQueueItem[]>([]);
  let loaded = $state(false);
  let isOpen = $state(false);
  let index = $state(0);
  let deciding = $state(false);

  let current = $derived(items.at(index));
  let done = $derived(index >= items.length);

  async function loadQueue() {
    try {
      const body = {
        species: '',
        speciesScientific: [],
        dateStart: '',
        dateEnd: '',
        confidenceMin: 0,
        confidenceMax: 1,
        verifiedStatus: 'unverified',
        lockedStatus: 'any',
        deviceFilter: '',
        timeOfDay: 'any',
        page: 1,
        sortBy: 'date_desc',
      };
      const data = await api.post<{ results: ReviewQueueItem[]; total: number }>(
        '/api/v2/search',
        body
      );
      items = data.results ?? [];
    } catch (error) {
      logger.error('Failed to load review queue', error);
      items = [];
    } finally {
      loaded = true;
    }
  }

  onMount(() => {
    void loadQueue();
  });

  function open() {
    index = 0;
    isOpen = true;
  }

  function close() {
    isOpen = false;
    onReviewed?.();
    void loadQueue(); // refresh the banner count after a session
  }

  // verdict === null means "Not sure": advance without persisting anything.
  async function decide(verdict: 'correct' | 'false_positive' | null) {
    const item = current;
    if (!item || deciding) return;
    if (verdict) {
      deciding = true;
      const ok = await setDetectionVerification(Number(item.id), verdict);
      deciding = false;
      if (!ok) return; // keep the user on this item if the save failed
    }
    index += 1;
  }
</script>

{#if loaded && items.length > 0}
  <div class="review-banner surface-card">
    <span class="flex items-center gap-2 text-sm">
      <ClipboardList class="size-5 text-primary" />
      {items.length === 1
        ? t('reviewQueue.bannerOne')
        : t('reviewQueue.banner', { count: items.length })}
    </span>
    <button type="button" class="btn btn-primary btn-sm" onclick={open}>
      {t('reviewQueue.review')}
    </button>
  </div>
{/if}

<Modal {isOpen} title={t('reviewQueue.title')} size="lg" onClose={close}>
  {#snippet children()}
    {#if done}
      <div class="space-y-3 text-center">
        <p class="text-base font-medium">{t('reviewQueue.allDone')}</p>
        <button type="button" class="btn btn-sm" onclick={close}>{t('reviewQueue.done')}</button>
      </div>
    {:else if current}
      <div class="space-y-3">
        <p class="text-xs text-base-content/60">
          {t('reviewQueue.progress', { current: index + 1, total: items.length })}
        </p>
        <div>
          <h3 class="text-base font-semibold">{current.commonName}</h3>
          <p class="text-sm italic text-base-content/60">{current.scientificName}</p>
        </div>

        {#if current.hasAudio}
          <SpectrogramPlayer
            audioUrl={buildAppUrl(`/api/v2/audio/${current.id}`)}
            detectionId={current.id}
            spectrogramSize="md"
          />
        {/if}

        <div class="grid grid-cols-3 gap-2 pt-1">
          <button
            type="button"
            class="btn btn-outline btn-error btn-sm gap-1"
            disabled={deciding}
            onclick={() => decide('false_positive')}
          >
            <X class="size-4" />{t('reviewQueue.wrong')}
          </button>
          <button
            type="button"
            class="btn btn-ghost btn-sm gap-1"
            disabled={deciding}
            onclick={() => decide(null)}
          >
            <CircleHelp class="size-4" />{t('reviewQueue.notSure')}
          </button>
          <button
            type="button"
            class="btn btn-success btn-sm gap-1"
            disabled={deciding}
            onclick={() => decide('correct')}
          >
            <Check class="size-4" />{t('reviewQueue.correct')}
          </button>
        </div>
      </div>
    {/if}
  {/snippet}
</Modal>

<style>
  .review-banner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    padding: 0.75rem 1rem;
  }
</style>
