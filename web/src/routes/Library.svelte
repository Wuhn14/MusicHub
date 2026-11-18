<script lang="ts">
  import {
    errorMessage,
    getLibraryAlbums,
    getLibraryArtists,
    getLibraryTracks,
    getScanStatus,
    isAbort,
    startScan,
    type AlbumSummary,
    type LibAlbum,
    type LibArtist,
    type LibrarySort,
    type LibTrack,
    type ScanStatus,
  } from '../lib/api';
  import { navigate, replaceQuery, type LibraryTab } from '../lib/router';
  import AlbumGrid from '../components/AlbumGrid.svelte';
  import Status from '../components/Status.svelte';
  import TrackList from '../components/TrackList.svelte';

  interface Props {
    tab: LibraryTab;
    sort: string;
    q: string;
    artist: string;
  }

  let { tab, sort, q, artist }: Props = $props();

  const sortOptions: { value: LibrarySort; label: string }[] = [
    { value: 'added', label: 'Ajoutés récemment' },
    { value: 'name', label: 'Nom' },
    { value: 'year', label: 'Année' },
  ];
  const tabs: { value: LibraryTab; label: string }[] = [
    { value: 'albums', label: 'Albums' },
    { value: 'artists', label: 'Artistes' },
    { value: 'tracks', label: 'Titres' },
  ];

  const sortValue = $derived<LibrarySort>(sort === 'name' || sort === 'year' ? sort : 'added');

  function libUrl(next: { tab: LibraryTab; sort?: string; artist?: string; q?: string }): string {
    const p = new URLSearchParams({ tab: next.tab });
    if (next.sort && next.sort !== 'added') p.set('sort', next.sort);
    if (next.artist) p.set('artist', next.artist);
    if (next.q) p.set('q', next.q);
    return `/library?${p}`;
  }

  // Data of the current tab.
  let loading = $state(true);
  let error = $state('');
  let albums = $state<LibAlbum[]>([]);
  let artists = $state<LibArtist[]>([]);
  let tracks = $state<LibTrack[]>([]);
  // The page remounts on every navigation, so the initial value is enough.
  // svelte-ignore state_referenced_locally
  let search = $state(q);
  let reloadCount = $state(0);

  // The track search is debounced: `appliedQ` is what was last sent.
  // svelte-ignore state_referenced_locally
  let appliedQ = $state(q);
  $effect(() => {
    const value = search.trim();
    if (value === appliedQ) return;
    const timer = setTimeout(() => {
      appliedQ = value;
      replaceQuery(value);
    }, 250);
    return () => clearTimeout(timer);
  });

  $effect(() => {
    void reloadCount;
    const controller = new AbortController();
    const signal = controller.signal;
    loading = true;
    error = '';
    let request: Promise<void>;
    if (tab === 'albums') {
      request = getLibraryAlbums(sortValue, signal).then((r) => void (albums = r));
    } else if (tab === 'artists') {
      request = getLibraryArtists(signal).then((r) => void (artists = r));
    } else {
      request = getLibraryTracks(sortValue, appliedQ, signal).then((r) => void (tracks = r));
    }
    request
      .then(() => void (loading = false))
      .catch((err: unknown) => {
        if (isAbort(err)) return;
        error = errorMessage(err);
        loading = false;
      });
    return () => controller.abort();
  });

  const shownAlbums = $derived(
    artist ? albums.filter((a) => a.artist.toLowerCase() === artist.toLowerCase()) : albums,
  );
  const gridAlbums = $derived<AlbumSummary[]>(
    shownAlbums.map((a) => ({
      id: a.key,
      title: a.title,
      artistId: '',
      artistName: a.artist,
      primaryType: '',
      secondaryTypes: [],
      firstReleaseDate: a.year,
    })),
  );
  const coverIds = $derived(new Map(albums.map((a) => [a.key, a.albumId])));

  // Scan status. Polled every 2 s, only while a scan runs (or was just asked).
  let scan = $state<ScanStatus | null>(null);
  let scanError = $state('');
  let waiting = $state(false);
  let pollTick = $state(0);
  // How many polls were spent waiting for a requested scan to start.
  let waitTicks = $state(0);

  $effect(() => {
    void pollTick;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    getScanStatus(controller.signal)
      .then((s) => {
        const wasRunning = scan?.running ?? false;
        scan = s;
        if (s.running) {
          waiting = false;
          timer = setTimeout(() => (pollTick += 1), 2000);
        } else if (waiting && waitTicks < 4) {
          // The scan was requested but has not started yet.
          waitTicks += 1;
          timer = setTimeout(() => (pollTick += 1), 2000);
        } else {
          waiting = false;
          if (wasRunning) reloadCount += 1;
        }
      })
      .catch((err: unknown) => {
        if (!isAbort(err)) scanError = errorMessage(err);
      });
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  });

  async function scanNow(): Promise<void> {
    scanError = '';
    try {
      await startScan();
      waiting = true;
      waitTicks = 0;
      pollTick += 1;
    } catch (err) {
      scanError = errorMessage(err);
    }
  }

  const busy = $derived(waiting || (scan?.running ?? false));

  function plural(n: number, one: string, many: string): string {
    return `${n} ${n > 1 ? many : one}`;
  }

  const lastReport = $derived.by(() => {
    if (!scan || !scan.lastRun) return '';
    const r = scan.lastReport;
    let text = plural(r.seen, 'fichier', 'fichiers');
    if (r.unreadable > 0) text += `, ${plural(r.unreadable, 'illisible', 'illisibles')}`;
    if (r.missing > 0) text += `, ${plural(r.missing, 'disparu', 'disparus')}`;
    return text;
  });

  const isEmpty = $derived(
    !loading &&
      !error &&
      !artist &&
      !appliedQ &&
      (tab === 'albums' ? albums.length === 0 : tab === 'artists' ? artists.length === 0 : tracks.length === 0),
  );
</script>

<svelte:head>
  <title>Bibliothèque · MusicHub</title>
</svelte:head>

<section>
  <header class="head">
    <h1>Bibliothèque</h1>
    <div class="scan">
      <button type="button" disabled={busy} onclick={scanNow}>
        {busy ? 'Analyse en cours…' : 'Analyser maintenant'}
      </button>
      {#if lastReport && !busy}
        <span class="hint">Dernière analyse : {lastReport}</span>
      {/if}
    </div>
  </header>
  {#if scanError}
    <p class="problem" role="alert">{scanError}</p>
  {:else if scan?.lastError && !busy}
    <p class="problem" role="alert">La dernière analyse a échoué : {scan.lastError}</p>
  {/if}

  <nav class="tabs" aria-label="Sections de la bibliothèque">
    {#each tabs as t (t.value)}
      <a
        href={libUrl({ tab: t.value, sort: t.value === 'artists' ? '' : sort })}
        class:active={tab === t.value}
        aria-current={tab === t.value ? 'page' : undefined}>{t.label}</a
      >
    {/each}
  </nav>

  <div class="controls">
    {#if tab !== 'artists'}
      <label class="sort">
        <span>Trier par</span>
        <select
          value={sortValue}
          onchange={(e) => navigate(libUrl({ tab, sort: e.currentTarget.value, artist, q: tab === 'tracks' ? appliedQ : '' }))}
        >
          {#each sortOptions as o (o.value)}
            <option value={o.value}>{o.label}</option>
          {/each}
        </select>
      </label>
    {/if}
    {#if tab === 'tracks'}
      <label class="find">
        <span class="visually-hidden">Filtrer les titres</span>
        <input type="search" placeholder="Filtrer les titres…" maxlength="200" autocomplete="off" bind:value={search} />
      </label>
    {/if}
    {#if tab === 'albums' && artist}
      <a class="chip" href={libUrl({ tab: 'albums', sort })} aria-label="Retirer le filtre sur {artist}">
        {artist} <span aria-hidden="true">×</span>
      </a>
    {/if}
  </div>

  <Status {loading} {error} />

  {#if isEmpty}
    <div class="empty">
      <h2>Ta bibliothèque est vide</h2>
      <p>
        Dépose des fichiers audio (MP3, FLAC, M4A, OGG, Opus, WAV…) dans le dossier de musique de MusicHub, puis
        clique sur « Analyser maintenant ». L’envoi de fichiers depuis cette page arrivera bientôt.
      </p>
    </div>
  {:else if !loading && !error}
    {#if tab === 'albums'}
      {#if gridAlbums.length === 0}
        <p class="none">Aucun album ne correspond.</p>
      {:else}
        <AlbumGrid
          albums={gridAlbums}
          hrefFor={(a) => `/library/album/${encodeURIComponent(a.id)}`}
          coverFor={(a) => coverIds.get(a.id) ?? ''}
        />
      {/if}
    {:else if tab === 'artists'}
      <ul class="artists">
        {#each artists as a (a.name)}
          <li>
            <a href={libUrl({ tab: 'albums', artist: a.name })}>
              <span class="name">{a.name}</span>
              <span class="count">{plural(a.albumCount, 'album', 'albums')} · {plural(a.trackCount, 'titre', 'titres')}</span>
            </a>
          </li>
        {/each}
      </ul>
    {:else if tracks.length === 0}
      <p class="none">Aucun titre ne correspond à « {appliedQ} ».</p>
    {:else}
      <TrackList {tracks} showAlbum />
    {/if}
  {/if}
</section>

<style>
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 12px 24px;
    margin-bottom: 16px;
  }

  h1 {
    font-size: 2rem;
    font-weight: 800;
    letter-spacing: -0.02em;
  }

  .scan {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px 14px;
  }

  .hint {
    font-size: 0.85rem;
    color: var(--text-muted);
  }

  .problem {
    margin-bottom: 12px;
    color: var(--danger);
  }

  button {
    padding: 10px 20px;
    border: 0;
    border-radius: 999px;
    background: var(--accent);
    color: var(--on-accent);
    font-weight: 700;
    cursor: pointer;
    transition: background-color 0.15s ease;
  }

  button:hover:not(:disabled) {
    background: var(--accent-strong);
  }

  button:disabled {
    opacity: 0.7;
    cursor: progress;
  }

  .tabs {
    display: flex;
    gap: 8px;
    margin-bottom: 16px;
    overflow-x: auto;
  }

  .tabs a {
    flex: none;
    padding: 8px 16px;
    border-radius: 999px;
    background: var(--surface);
    color: var(--text-muted);
    font-weight: 600;
  }

  .tabs a:hover {
    color: var(--text);
    text-decoration: none;
    background: var(--surface-hover);
  }

  .tabs a.active {
    background: var(--text);
    color: var(--bg);
  }

  .controls {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 12px 16px;
    margin-bottom: 20px;
  }

  .controls:empty {
    display: none;
  }

  .sort {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--text-muted);
    font-size: 0.9rem;
  }

  select,
  input[type='search'] {
    min-width: 0;
    padding: 8px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
  }

  .find {
    flex: 1 1 220px;
    max-width: 420px;
  }

  .find input {
    width: 100%;
  }

  .chip {
    padding: 6px 12px;
    border: 1px solid var(--accent);
    border-radius: 999px;
    color: var(--accent);
    font-weight: 600;
    font-size: 0.9rem;
  }

  .chip:hover {
    text-decoration: none;
    background: var(--surface);
  }

  .empty {
    max-width: 560px;
    padding: 32px 0;
  }

  .empty h2 {
    margin-bottom: 8px;
    font-size: 1.3rem;
  }

  .empty p,
  .none {
    color: var(--text-muted);
  }

  .artists {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
    gap: 10px;
  }

  .artists a {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 14px 16px;
    border-radius: var(--radius-lg);
    background: var(--bg-elevated);
  }

  .artists a:hover {
    text-decoration: none;
    background: var(--surface-hover);
  }

  .name {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .count {
    font-size: 0.85rem;
    color: var(--text-muted);
  }
</style>
