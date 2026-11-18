<script lang="ts">
  // One file of "À identifier": what was read from it, a short listen, the
  // proposed recordings, a catalog search, the local track form and, for an
  // uploaded file, deletion.
  import {
    chooseRecording,
    deletePendingFile,
    errorMessage,
    isAbort,
    search,
    type IdentifyCandidate,
    type IdentifyFile,
    type TrackHit,
  } from '../lib/api';
  import { formatDuration } from '../lib/format';
  import { player } from '../lib/player.svelte';
  import Icon from './Icon.svelte';
  import LocalTrackForm from './LocalTrackForm.svelte';

  interface Props {
    file: IdentifyFile;
    /** Called after a successful action; message is shown on the page. */
    ondone: (id: string, message: string) => void;
  }

  let { file, ondone }: Props = $props();

  type Panel = 'none' | 'search' | 'local';
  let panel = $state<Panel>('none');
  let busy = $state(false);
  /** What the card is doing while busy. */
  let working = $state('');
  let error = $state('');

  // Catalog search.
  let query = $state('');
  let results = $state<TrackHit[]>([]);
  let searching = $state(false);
  let searched = $state(false);
  let searchError = $state('');
  let controller: AbortController | undefined;

  let dialog = $state<HTMLDialogElement>();

  const uid = $derived(`card-${file.id}`);
  const tagLine = $derived(
    [file.tags.artist || file.tags.albumArtist, file.tags.album, file.tags.title].filter(Boolean).join(' · '),
  );
  const isPlaying = $derived(player.current?.track.fileId === file.id && player.playing);

  function listen(): void {
    if (player.current?.track.fileId === file.id) {
      player.toggle();
      return;
    }
    player.playList([
      {
        fileId: file.id,
        title: file.tags.title || file.name,
        artist: file.tags.artist || file.tags.albumArtist || 'Fichier à identifier',
        album: file.tags.album,
        coverUrl: '',
        durationMs: file.durationMs,
      },
    ]);
  }

  async function choose(recordingId: string, albumId: string): Promise<void> {
    if (busy) {
      return;
    }
    busy = true;
    working = 'Enregistrement et rangement du fichier…';
    error = '';
    try {
      const res = await chooseRecording(file.id, recordingId, albumId);
      let message = '';
      if (res.duplicate === 'discarded-new') {
        message = `« ${file.name} » était un doublon de moins bonne qualité : il a été supprimé.`;
      } else if (res.duplicate === 'hidden-new') {
        message = `« ${file.name} » est un doublon d’un titre déjà présent : il est masqué.`;
      }
      ondone(file.id, message);
    } catch (err) {
      error = errorMessage(err);
      busy = false;
    }
  }

  function candidateLabel(c: IdentifyCandidate): string {
    return [c.title, c.artist].filter(Boolean).join(' — ');
  }

  function candidateMeta(c: IdentifyCandidate): string {
    return [c.album, formatDuration(c.lengthMs)].filter(Boolean).join(' · ');
  }

  const sourceLabels: Record<string, string> = {
    acoustid: 'Empreinte AcoustID',
    search: 'Recherche par tags',
    tag: 'Identifiant des tags',
  };

  function openSearch(): void {
    panel = panel === 'search' ? 'none' : 'search';
    if (panel === 'search' && !query) {
      query = [file.tags.artist || file.tags.albumArtist, file.tags.title].filter(Boolean).join(' ');
      if (query) {
        void runSearch();
      }
    }
  }

  async function runSearch(e?: SubmitEvent): Promise<void> {
    e?.preventDefault();
    const q = query.trim();
    if (!q) {
      return;
    }
    controller?.abort();
    controller = new AbortController();
    searching = true;
    searchError = '';
    try {
      results = (await search(q, controller.signal)).tracks;
      searched = true;
    } catch (err) {
      if (isAbort(err)) {
        return;
      }
      searchError = errorMessage(err);
    }
    searching = false;
  }

  function askDelete(): void {
    error = '';
    dialog?.showModal();
  }

  async function confirmDelete(): Promise<void> {
    busy = true;
    working = 'Suppression…';
    try {
      await deletePendingFile(file.id);
      dialog?.close();
      ondone(file.id, `« ${file.name} » a été supprimé.`);
    } catch (err) {
      dialog?.close();
      error = errorMessage(err);
      busy = false;
    }
  }

  function created(coverError: string): void {
    ondone(file.id, coverError);
  }
</script>

<article class="card" aria-labelledby="{uid}-name" aria-busy={busy}>
  <header class="head">
    <button
      type="button"
      class="listen"
      class:on={isPlaying}
      onclick={listen}
      aria-label={isPlaying ? `Mettre en pause ${file.name}` : `Écouter ${file.name}`}
    >
      <Icon name={isPlaying ? 'pause' : 'play'} size={22} />
    </button>
    <div class="what">
      <h2 id="{uid}-name" class="name" title={file.name}>{file.name}</h2>
      <p class="tags">
        {#if tagLine}
          {tagLine}
        {:else}
          <span class="faint">Aucun tag lisible</span>
        {/if}
      </p>
      <p class="meta">
        {#if file.durationMs > 0}<span>{formatDuration(file.durationMs)}</span>{/if}
        {#if file.format}<span>{file.format.toUpperCase()}</span>{/if}
        <span class="origin">{file.origin === 'uploaded' ? 'Envoyé' : 'Dans ton dossier musique'}</span>
      </p>
    </div>
  </header>

  {#if file.candidates.length > 0}
    <section aria-label="Propositions">
      <h3 class="label">Propositions</h3>
      <ul class="cands">
        {#each file.candidates as c (c.recordingId)}
          <li class="cand">
            <div class="cand-text">
              <span class="cand-title">{candidateLabel(c)}</span>
              {#if candidateMeta(c)}<span class="cand-meta">{candidateMeta(c)}</span>{/if}
            </div>
            <span class="score" title={sourceLabels[c.source] ?? c.source}>
              {#if c.source === 'acoustid'}
                <svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path fill="currentColor" d="M3 11h2v2H3v-2Zm4-4h2v10H7V7Zm4-4h2v18h-2V3Zm4 6h2v6h-2V9Zm4 2h2v2h-2v-2Z" /></svg>
              {:else}
                <svg viewBox="0 0 24 24" width="12" height="12" aria-hidden="true"><path fill="currentColor" d="M10.5 3a7.5 7.5 0 0 1 5.96 12.05l4.74 4.74-1.41 1.41-4.74-4.74A7.5 7.5 0 1 1 10.5 3Zm0 2a5.5 5.5 0 1 0 0 11 5.5 5.5 0 0 0 0-11Z" /></svg>
              {/if}
              <span class="sr-only">{sourceLabels[c.source] ?? c.source}, score</span>
              {c.score} %
            </span>
            <button
              type="button"
              class="choose"
              disabled={busy}
              onclick={() => choose(c.recordingId, c.albumId)}
              aria-label={`Choisir ${candidateLabel(c)}`}>Choisir</button
            >
          </li>
        {/each}
      </ul>
    </section>
  {:else}
    <p class="none">Aucune proposition trouvée automatiquement.</p>
  {/if}

  <div class="actions">
    <button type="button" class="action" aria-expanded={panel === 'search'} onclick={openSearch} disabled={busy}>
      Chercher dans le catalogue
    </button>
    <button
      type="button"
      class="action"
      aria-expanded={panel === 'local'}
      onclick={() => (panel = panel === 'local' ? 'none' : 'local')}
      disabled={busy}
    >
      Créer un titre local
    </button>
    {#if file.origin === 'uploaded'}
      <button type="button" class="action danger" onclick={askDelete} disabled={busy}>Supprimer</button>
    {/if}
  </div>

  {#if error}
    <p class="bad" role="alert">{error}</p>
  {:else if busy}
    <p class="working" role="status"><span class="spinner" aria-hidden="true"></span>{working}</p>
  {/if}

  {#if panel === 'search'}
    <div class="search">
      <form class="search-form" role="search" onsubmit={runSearch}>
        <label class="sr-only" for="{uid}-q">Rechercher un titre dans le catalogue</label>
        <input id="{uid}-q" type="search" bind:value={query} placeholder="Artiste et titre" autocomplete="off" />
        <button type="submit" class="action" disabled={searching || !query.trim()}>Rechercher</button>
      </form>
      {#if searchError}
        <p class="bad" role="alert">{searchError}</p>
      {:else if searching}
        <p class="faint" role="status">Recherche…</p>
      {:else if searched && results.length === 0}
        <p class="faint">Aucun titre trouvé.</p>
      {:else if results.length > 0}
        <ul class="cands" aria-label="Résultats du catalogue">
          {#each results as r (r.recordingId + r.albumId)}
            <li class="cand">
              <div class="cand-text">
                <span class="cand-title">{[r.title, r.artistName].filter(Boolean).join(' — ')}</span>
                <span class="cand-meta">{[r.albumTitle, formatDuration(r.lengthMs)].filter(Boolean).join(' · ')}</span>
              </div>
              <button
                type="button"
                class="choose"
                disabled={busy}
                onclick={() => choose(r.recordingId, r.albumId)}
                aria-label={`Choisir ${r.title}${r.artistName ? ' — ' + r.artistName : ''}`}>Choisir</button
              >
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  {:else if panel === 'local'}
    <LocalTrackForm fileId={file.id} tags={file.tags} oncreated={created} oncancel={() => (panel = 'none')} />
  {/if}

  {#if file.origin === 'uploaded'}
    <dialog bind:this={dialog} class="confirm" aria-labelledby="{uid}-confirm">
      <h3 id="{uid}-confirm">Supprimer ce fichier ?</h3>
      <p>« {file.name} » sera définitivement supprimé de MusicHub.</p>
      <div class="dialog-actions">
        <button type="button" class="action" onclick={() => dialog?.close()} disabled={busy}>Annuler</button>
        <button type="button" class="action danger-fill" onclick={confirmDelete} disabled={busy}>Supprimer</button>
      </div>
    </dialog>
  {/if}
</article>

<style>
  .card {
    display: flex;
    flex-direction: column;
    gap: 14px;
    padding: 16px;
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    background: var(--bg-elevated);
  }

  .head {
    display: flex;
    align-items: flex-start;
    gap: 14px;
  }

  .listen {
    flex: none;
    display: grid;
    place-items: center;
    width: 44px;
    height: 44px;
    border: 0;
    border-radius: 50%;
    background: var(--surface);
    color: var(--text);
    cursor: pointer;
    transition: background-color 0.15s ease, color 0.15s ease;
  }

  .listen:hover,
  .listen.on {
    background: var(--accent);
    color: var(--on-accent);
  }

  .what {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .name {
    overflow: hidden;
    font-size: 1.05rem;
    font-weight: 700;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tags {
    color: var(--text-muted);
    overflow-wrap: anywhere;
  }

  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 12px;
    font-size: 0.82rem;
    color: var(--text-faint);
  }

  .origin {
    padding: 0 8px;
    border-radius: 999px;
    background: var(--surface);
    color: var(--text-muted);
  }

  .label {
    margin-bottom: 6px;
    font-size: 0.78rem;
    font-weight: 700;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--text-faint);
  }

  .cands {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .cand {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 8px 10px;
    border-radius: var(--radius);
  }

  .cand:hover {
    background: var(--surface);
  }

  .cand-text {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }

  .cand-title {
    font-weight: 600;
    overflow-wrap: anywhere;
  }

  .cand-meta {
    font-size: 0.85rem;
    color: var(--text-muted);
    overflow-wrap: anywhere;
  }

  .score {
    flex: none;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 2px 8px;
    border-radius: 999px;
    background: var(--surface);
    color: var(--text-muted);
    font-size: 0.78rem;
    font-variant-numeric: tabular-nums;
  }

  .none,
  .faint {
    color: var(--text-faint);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  button {
    font-weight: 700;
    cursor: pointer;
  }

  button:disabled {
    opacity: 0.55;
    cursor: default;
  }

  .action,
  .choose {
    min-height: 36px;
    padding: 6px 14px;
    border: 1px solid var(--border);
    border-radius: 999px;
    background: transparent;
    font-size: 0.9rem;
  }

  .action:hover:not(:disabled),
  .action[aria-expanded='true'] {
    border-color: var(--text-muted);
    background: var(--surface);
  }

  .choose {
    flex: none;
    border-color: var(--accent);
    color: var(--accent);
  }

  .choose:hover:not(:disabled) {
    background: var(--accent);
    color: var(--on-accent);
  }

  .danger {
    color: var(--danger);
  }

  .danger-fill {
    border-color: var(--danger);
    background: var(--danger);
    color: #1a0606;
  }

  .bad {
    color: var(--danger);
  }

  .working {
    display: flex;
    align-items: center;
    gap: 10px;
    color: var(--text-muted);
  }

  .spinner {
    width: 16px;
    height: 16px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .spinner {
      animation-duration: 2.4s;
    }
  }

  .search {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .search-form {
    display: flex;
    gap: 8px;
  }

  .search-form input {
    flex: 1;
    min-width: 0;
    min-height: 40px;
    padding: 8px 12px;
    border: 1px solid var(--border);
    border-radius: 999px;
    background: var(--bg);
  }

  .search-form input:focus-visible {
    border-color: var(--accent);
    outline: none;
  }

  .confirm {
    max-width: min(420px, calc(100vw - 32px));
    padding: 20px;
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    background: var(--bg-elevated);
    color: var(--text);
  }

  .confirm::backdrop {
    background: rgb(0 0 0 / 0.6);
  }

  .confirm h3 {
    margin-bottom: 8px;
    font-size: 1.1rem;
  }

  .confirm p {
    color: var(--text-muted);
    overflow-wrap: anywhere;
  }

  .dialog-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 18px;
  }

  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }

  @media (max-width: 560px) {
    .cand {
      flex-wrap: wrap;
      padding: 8px 4px;
    }

    .cand-text {
      flex-basis: 100%;
    }

    .cand .choose {
      margin-left: auto;
    }
  }
</style>
