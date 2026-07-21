<!--
  CompareSounds.svelte

  A "Compare sounds" modal: shows two sound pictures ("Your recording" vs a
  "Trusted example"), plays them back-to-back on one tap, and asks the one simple
  question — does the identification seem right? Correct / Not sure / Wrong.

  When the user is unsure (or thinks it's another bird) it offers the closest
  alternative species and lets them A/B the local recording against each one.

  It reuses existing endpoints and degrades quietly; decisions go through the
  same review wrapper as the rest of the app.
-->
<script lang="ts">
  import { onDestroy } from 'svelte';
  import type {
    AlternativeSpecies,
    Detection,
    ReferenceRecording,
  } from '$lib/types/detection.types';
  import Modal from '$lib/desktop/components/ui/Modal.svelte';
  import {
    fetchReference,
    fetchAlternatives,
    referenceSourceLabel,
  } from '$lib/utils/referenceRecording';
  import { setDetectionVerification } from '$lib/utils/reviewDetection';
  import { createSpectrogramLoader } from '$lib/utils/spectrogramLoader.svelte';
  import { buildAppUrl } from '$lib/utils/urlHelpers';
  import { t } from '$lib/i18n';
  import { Play, Square, Check, CircleHelp, X, ArrowLeft } from '@lucide/svelte';

  interface Props {
    detection: Detection;
    isOpen: boolean;
    onClose: () => void;
    onReviewed?: (_verdict: 'correct' | 'false_positive') => void;
  }

  let { detection, isOpen, onClose, onReviewed }: Props = $props();

  type Step = 'compare' | 'alternatives' | 'wrongReason' | 'notSure';
  type Which = 'local' | 'example';

  let step = $state<Step>('compare');
  let reference = $state<ReferenceRecording | null>(null);
  let loadingRef = $state(true);
  let deciding = $state(false);

  // The example currently shown on the right — the detected species by default,
  // or an alternative species the user chose to compare against.
  let activeExample = $state<ReferenceRecording | null>(null);
  let comparingAltName = $state<string | null>(null);

  let alternatives = $state<AlternativeSpecies[]>([]);
  let loadingAlts = $state(false);

  const loader = createSpectrogramLoader({ size: 'md', raw: true });

  let localAudio = $state<HTMLAudioElement>();
  let exampleAudio = $state<HTMLAudioElement>();
  let autoPlaying = $state(false);
  let playingWhich = $state<Which | null>(null);

  let localAudioUrl = $derived(buildAppUrl(`/api/v2/audio/${detection.id}`));
  let source = $derived(referenceSourceLabel(activeExample?.sourceProvider));
  let canCompare = $derived(!!activeExample?.audioUrl);
  let exampleLabel = $derived(comparingAltName ?? t('reference.title'));

  // Back-to-back sequence: your recording, the example, then once more.
  const SEQUENCE: Which[] = ['local', 'example', 'local', 'example'];
  let seqIndex = 0;

  $effect(() => {
    if (!isOpen) return;
    step = 'compare';
    loadingRef = true;
    reference = null;
    activeExample = null;
    comparingAltName = null;
    alternatives = [];
    loader.start(detection.id);
    let cancelled = false;
    void fetchReference(detection.id).then(r => {
      if (!cancelled) {
        reference = r?.enabled ? (r.best ?? null) : null;
        activeExample = reference;
        loadingRef = false;
      }
    });
    return () => {
      cancelled = true;
      stopPlayback();
      loader.stop();
    };
  });

  onDestroy(() => loader.destroy());

  function elFor(which: Which): HTMLAudioElement | undefined {
    return which === 'local' ? localAudio : exampleAudio;
  }

  // safePlay tolerates environments (jsdom tests) where play() is unavailable.
  function safePlay(el: HTMLAudioElement, onFail: () => void) {
    try {
      const result: unknown = el.play();
      if (result && typeof (result as Promise<void>).then === 'function') {
        (result as Promise<void>).then(() => {}, onFail);
      }
    } catch {
      onFail();
    }
  }

  function stopPlayback() {
    autoPlaying = false;
    playingWhich = null;
    for (const el of [localAudio, exampleAudio]) {
      if (el) {
        el.pause();
        el.onended = null;
      }
    }
  }

  function playOne(which: Which) {
    stopPlayback();
    const el = elFor(which);
    if (!el) return;
    playingWhich = which;
    el.currentTime = 0;
    el.onended = () => {
      playingWhich = null;
    };
    safePlay(el, () => {
      playingWhich = null;
    });
  }

  function toggleAutoCompare() {
    if (autoPlaying) {
      stopPlayback();
      return;
    }
    if (!localAudio || !exampleAudio) return;
    autoPlaying = true;
    seqIndex = 0;
    playSequenceStep();
  }

  function playSequenceStep() {
    const which = SEQUENCE.at(seqIndex);
    if (!autoPlaying || which === undefined) {
      stopPlayback();
      return;
    }
    const el = elFor(which);
    if (!el) {
      stopPlayback();
      return;
    }
    playingWhich = which;
    el.currentTime = 0;
    el.onended = () => {
      el.onended = null;
      seqIndex += 1;
      playSequenceStep();
    };
    safePlay(el, () => stopPlayback());
  }

  async function decide(verdict: 'correct' | 'false_positive') {
    if (deciding) return;
    deciding = true;
    stopPlayback();
    const ok = await setDetectionVerification(detection.id, verdict);
    deciding = false;
    if (ok) {
      onReviewed?.(verdict);
      onClose();
    }
  }

  function showAlternatives() {
    stopPlayback();
    step = 'alternatives';
    if (alternatives.length > 0 || loadingAlts) return;
    loadingAlts = true;
    void fetchAlternatives(detection.id).then(r => {
      alternatives = r?.enabled ? (r.alternatives ?? []) : [];
      loadingAlts = false;
    });
  }

  function chooseAlternative(alt: AlternativeSpecies) {
    stopPlayback();
    activeExample = alt.example ?? null;
    comparingAltName = alt.commonName ?? alt.scientificName;
    step = 'compare';
  }

  function backToDetected() {
    stopPlayback();
    activeExample = reference;
    comparingAltName = null;
    step = 'compare';
  }

  function handleClose() {
    stopPlayback();
    onClose();
  }
</script>

<Modal {isOpen} title={t('reference.compare.title')} size="lg" onClose={handleClose}>
  {#snippet children()}
    <div class="space-y-4">
      <p class="text-sm text-base-content/70">{detection.commonName}</p>

      {#if loadingRef}
        <div class="flex items-center gap-2 text-sm text-base-content/60" role="status">
          <span class="loading loading-spinner loading-sm" aria-hidden="true"></span>
          <span>{t('reference.loading')}</span>
        </div>
      {:else if !reference}
        <p class="text-sm text-base-content/70">{t('reference.compare.noExample')}</p>
      {:else if step === 'compare'}
        {#if comparingAltName}
          <button type="button" class="btn btn-ghost btn-xs gap-1" onclick={backToDetected}>
            <ArrowLeft class="size-3" />
            {t('reference.compare.backToDetected', { species: detection.commonName })}
          </button>
        {/if}

        <!-- Auto-compare -->
        <button
          type="button"
          class="btn btn-primary btn-sm gap-2"
          disabled={!canCompare}
          onclick={toggleAutoCompare}
        >
          {#if autoPlaying}
            <Square class="size-4" />{t('reference.compare.stop')}
          {:else}
            <Play class="size-4" />{t('reference.compare.auto')}
          {/if}
        </button>

        <!-- Two sound pictures -->
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div class="sound-picture" class:ring-2={playingWhich === 'local'}>
            <span class="sound-picture-label">{t('reference.compare.yourRecording')}</span>
            <div class="sound-picture-frame">
              {#if loader.spectrogramUrl}
                <img
                  src={loader.spectrogramUrl}
                  alt={t('reference.compare.yourRecording')}
                  class="sound-picture-img"
                  onload={() => loader.handleImageLoad()}
                  onerror={() => loader.handleImageError()}
                />
              {:else}
                <span class="loading loading-spinner loading-sm" aria-hidden="true"></span>
              {/if}
            </div>
            <button
              type="button"
              class="btn btn-ghost btn-xs gap-1"
              onclick={() => playOne('local')}
            >
              <Play class="size-3" />{t('reference.compare.play')}
            </button>
          </div>

          <div class="sound-picture" class:ring-2={playingWhich === 'example'}>
            <span class="sound-picture-label">{exampleLabel}</span>
            <div class="sound-picture-frame">
              {#if activeExample?.sonogramUrl}
                <img src={activeExample.sonogramUrl} alt={exampleLabel} class="sound-picture-img" />
              {:else}
                <span class="text-xs text-base-content/50"
                  >{t('reference.compare.noSoundPicture')}</span
                >
              {/if}
            </div>
            <button
              type="button"
              class="btn btn-ghost btn-xs gap-1"
              disabled={!canCompare}
              onclick={() => playOne('example')}
            >
              <Play class="size-3" />{t('reference.compare.play')}
            </button>
          </div>
        </div>

        {#if activeExample}
          <p class="text-xs text-base-content/60">
            {t('reference.attribution', { source, recordist: activeExample.recordist ?? '' })}
          </p>
        {/if}

        <!-- The one decision -->
        <div class="flex flex-wrap gap-2 pt-2">
          <button
            type="button"
            class="btn btn-success btn-sm gap-1"
            disabled={deciding}
            onclick={() => decide('correct')}
          >
            <Check class="size-4" />{t('reference.compare.keep')}
          </button>
          <button
            type="button"
            class="btn btn-ghost btn-sm gap-1"
            disabled={deciding}
            onclick={showAlternatives}
          >
            <CircleHelp class="size-4" />{t('reference.compare.notSure')}
          </button>
          <button
            type="button"
            class="btn btn-outline btn-error btn-sm gap-1"
            disabled={deciding}
            onclick={() => (step = 'wrongReason')}
          >
            <X class="size-4" />{t('reference.compare.wrong')}
          </button>
        </div>
      {:else if step === 'alternatives'}
        <p class="text-sm font-medium">{t('reference.compare.alternativesTitle')}</p>
        {#if loadingAlts}
          <div class="flex items-center gap-2 text-sm text-base-content/60" role="status">
            <span class="loading loading-spinner loading-sm" aria-hidden="true"></span>
            <span>{t('reference.loading')}</span>
          </div>
        {:else}
          <div class="flex flex-col gap-2">
            {#each alternatives as alt (alt.scientificName)}
              <div class="alt-row">
                <span>{alt.commonName || alt.scientificName}</span>
                <button
                  type="button"
                  class="btn btn-ghost btn-xs"
                  disabled={!alt.example?.audioUrl}
                  onclick={() => chooseAlternative(alt)}
                >
                  {alt.example?.audioUrl
                    ? t('reference.compare.compareWith')
                    : t('reference.compare.noExampleForAlt')}
                </button>
              </div>
            {/each}
            {#if alternatives.length === 0}
              <p class="text-sm text-base-content/70">{t('reference.compare.noAlternatives')}</p>
            {/if}
          </div>
        {/if}
        <button type="button" class="btn btn-ghost btn-xs mt-1" onclick={() => (step = 'notSure')}
          >{t('reference.compare.stillNotSure')}</button
        >
      {:else if step === 'wrongReason'}
        <p class="text-sm font-medium">{t('reference.compare.wrongReason.title')}</p>
        <div class="flex flex-col gap-2">
          <button
            type="button"
            class="btn btn-outline btn-sm justify-start"
            disabled={deciding}
            onclick={showAlternatives}
          >
            {t('reference.compare.wrongReason.anotherBird')}
          </button>
          {#each ['onlyNoise', 'overlap', 'unclear'] as reason (reason)}
            <button
              type="button"
              class="btn btn-outline btn-sm justify-start"
              disabled={deciding}
              onclick={() => decide('false_positive')}
            >
              {t(`reference.compare.wrongReason.${reason}`)}
            </button>
          {/each}
        </div>
        <button type="button" class="btn btn-ghost btn-xs mt-1" onclick={() => (step = 'compare')}
          >{t('reference.compare.back')}</button
        >
      {:else if step === 'notSure'}
        <p class="text-sm text-base-content/80">{t('reference.compare.notSureNote')}</p>
        <button type="button" class="btn btn-sm mt-2" onclick={handleClose}
          >{t('reference.compare.done')}</button
        >
      {/if}

      <!-- Hidden audio elements drive playback -->
      <audio bind:this={localAudio} src={localAudioUrl} preload="none" class="hidden"></audio>
      {#if activeExample?.audioUrl}
        <audio bind:this={exampleAudio} src={activeExample.audioUrl} preload="none" class="hidden"
        ></audio>
      {/if}
    </div>
  {/snippet}
</Modal>

<style>
  .sound-picture {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.375rem;
    border-radius: 0.5rem;
    padding: 0.5rem;
    background-color: var(--color-base-200);
  }

  .sound-picture-label {
    font-size: 0.75rem;
    font-weight: 600;
    color: color-mix(in srgb, var(--color-base-content) 70%, transparent);
  }

  .sound-picture-frame {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 100%;
    min-height: 5rem;
    overflow: hidden;
    border-radius: 0.375rem;
    background-color: var(--color-base-300);
  }

  .sound-picture-img {
    width: 100%;
    height: auto;
    image-rendering: pixelated;
  }

  .alt-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    padding: 0.375rem 0.5rem;
    border-radius: 0.375rem;
    background-color: var(--color-base-200);
    font-size: 0.875rem;
  }
</style>
