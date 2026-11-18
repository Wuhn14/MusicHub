<script lang="ts">
  // "À identifier": the files the automatic identification could not match.
  import { onDestroy } from 'svelte';
  import { flip } from 'svelte/animate';
  import { slide } from 'svelte/transition';
  import IdentifyCard from '../components/IdentifyCard.svelte';
  import Status from '../components/Status.svelte';
  import { errorMessage, getIdentifyList, isAbort, type IdentifyFile } from '../lib/api';
  import { identifyCount } from '../lib/identify.svelte';

  /** List refresh while files are still being identified automatically. */
  const busyRefreshMs = 5000;
  /** List refresh otherwise (new uploads, scans). */
  const idleRefreshMs = 30_000;

  let files = $state<IdentifyFile[]>([]);
  let total = $state(0);
  let queued = $state(0);
  let loading = $state(true);
  let error = $state('');
  let notice = $state('');

  const reducedMotion =
    typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  const motion = reducedMotion ? 0 : 260;

  let controller: AbortController | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let destroyed = false;

  async function load(): Promise<void> {
    clearTimeout(timer);
    controller?.abort();
    controller = new AbortController();
    try {
      const list = await getIdentifyList(controller.signal);
      files = list.files;
      total = list.total;
      queued = list.queued;
      identifyCount.count = list.total;
      identifyCount.queued = list.queued;
      error = '';
    } catch (err) {
      if (isAbort(err)) {
        return;
      }
      // Keep showing the cards already loaded; only an empty page shows the error.
      error = errorMessage(err);
    } finally {
      loading = false;
    }
    if (!destroyed) {
      timer = setTimeout(() => void load(), queued > 0 ? busyRefreshMs : idleRefreshMs);
    }
  }

  void load();

  onDestroy(() => {
    destroyed = true;
    clearTimeout(timer);
    controller?.abort();
  });

  function done(id: string, message: string): void {
    files = files.filter((f) => f.id !== id);
    total = Math.max(0, total - 1);
    identifyCount.count = total;
    notice = message;
    void identifyCount.refresh();
    // The list holds at most 100 files: fetch the next ones when it runs low.
    if (files.length === 0 && total > 0) {
      void load();
    }
  }

  const plural = (n: number, one: string, many: string) => `${n} ${n > 1 ? many : one}`;
</script>

<section class="identify">
  <header class="top">
    <h1>À identifier</h1>
    {#if !loading && !error}
      <p class="summary" aria-live="polite">
        {#if total > 0}
          {plural(total, 'fichier attend', 'fichiers attendent')} ton avis.
        {/if}
        {#if queued > 0}
          <span class="queued">
            <span class="dot" aria-hidden="true"></span>
            {plural(queued, 'fichier', 'fichiers')} en cours d’identification automatique…
          </span>
        {/if}
      </p>
    {/if}
  </header>

  {#if notice}
    <div class="notice" role="status">
      <span>{notice}</span>
      <button type="button" class="dismiss" onclick={() => (notice = '')} aria-label="Fermer le message">×</button>
    </div>
  {/if}

  {#if loading || (error && files.length === 0)}
    <Status {loading} {error} />
  {:else if files.length === 0}
    <div class="empty">
      <p class="empty-title">Tout est identifié 🎉</p>
      <p class="empty-text">
        {#if queued > 0}
          Les fichiers en cours d’identification apparaîtront ici s’ils ont besoin de toi.
        {:else}
          Les fichiers que MusicHub ne reconnaît pas seuls apparaîtront ici.
        {/if}
        <a href="/upload">Ajouter de la musique</a>
      </p>
    </div>
  {:else}
    {#if error}
      <p class="stale" role="alert">{error}</p>
    {/if}
    <ul class="list" aria-label="Fichiers à identifier">
      {#each files as file (file.id)}
        <li animate:flip={{ duration: motion }} out:slide={{ duration: motion }}>
          <IdentifyCard {file} ondone={done} />
        </li>
      {/each}
    </ul>
    {#if total > files.length}
      <p class="more">{plural(total - files.length, 'autre fichier', 'autres fichiers')} après ceux-ci.</p>
    {/if}
  {/if}
</section>

<style>
  .identify {
    max-width: 860px;
  }

  .top {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-bottom: 24px;
  }

  h1 {
    font-size: 2rem;
    font-weight: 800;
    letter-spacing: -0.02em;
  }

  .summary {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px 16px;
    color: var(--text-muted);
  }

  .queued {
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }

  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
    animation: pulse 1.4s ease-in-out infinite;
  }

  @keyframes pulse {
    50% {
      opacity: 0.3;
    }
  }

  .notice {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 16px;
    padding: 10px 14px;
    border: 1px solid color-mix(in srgb, var(--accent) 35%, transparent);
    border-radius: var(--radius);
    background: color-mix(in srgb, var(--accent) 8%, var(--bg-elevated));
  }

  .dismiss {
    flex: none;
    width: 32px;
    height: 32px;
    border: 0;
    border-radius: 50%;
    background: transparent;
    color: var(--text-muted);
    font-size: 1.3rem;
    line-height: 1;
    cursor: pointer;
  }

  .dismiss:hover {
    background: var(--surface-hover);
    color: var(--text);
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .empty {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 48px 24px;
    border: 1px dashed var(--border);
    border-radius: var(--radius-lg);
    text-align: center;
  }

  .empty-title {
    font-size: 1.3rem;
    font-weight: 800;
  }

  .empty-text {
    color: var(--text-muted);
  }

  .empty-text a {
    color: var(--accent);
    font-weight: 600;
    margin-left: 4px;
  }

  .stale {
    margin-bottom: 12px;
    color: var(--danger);
  }

  .more {
    margin-top: 16px;
    color: var(--text-faint);
  }

  @media (prefers-reduced-motion: reduce) {
    .dot {
      animation: none;
    }
  }
</style>
