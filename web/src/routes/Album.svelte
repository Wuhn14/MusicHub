<script lang="ts">
  import { errorMessage, getAlbum, isAbort, type Album, type Track } from '../lib/api';
  import { formatDuration, formatTotal, typeLabel, yearOf } from '../lib/format';
  import Cover from '../components/Cover.svelte';
  import Status from '../components/Status.svelte';

  interface Props {
    id: string;
  }

  let { id }: Props = $props();

  let album = $state<Album | null>(null);
  let loading = $state(true);
  let error = $state('');

  $effect(() => {
    const controller = new AbortController();
    album = null;
    error = '';
    loading = true;
    getAlbum(id, controller.signal)
      .then((res) => {
        album = res;
        loading = false;
      })
      .catch((err: unknown) => {
        if (isAbort(err)) {
          return;
        }
        error = errorMessage(err);
        loading = false;
      });
    return () => controller.abort();
  });

  const discs = $derived.by(() => {
    const byDisc = new Map<number, Track[]>();
    for (const track of album?.tracks ?? []) {
      const list = byDisc.get(track.disc) ?? [];
      list.push(track);
      byDisc.set(track.disc, list);
    }
    return [...byDisc.entries()]
      .sort(([a], [b]) => a - b)
      .map(([disc, tracks]) => ({ disc, tracks: tracks.sort((a, b) => a.position - b.position) }));
  });

  const totalMs = $derived((album?.tracks ?? []).reduce((sum, t) => sum + (t.lengthMs > 0 ? t.lengthMs : 0), 0));

  const kind = $derived(
    album
      ? album.secondaryTypes.length > 0
        ? album.secondaryTypes.map(typeLabel).join(' · ')
        : typeLabel(album.primaryType || 'Album')
      : '',
  );
</script>

<svelte:head>
  <title>{album ? `${album.title} · ${album.artistName}` : 'Album'} · MusicHub</title>
</svelte:head>

{#if album}
  <header class="header">
    <div class="art">
      <Cover albumId={album.id} size={500} alt="Pochette de {album.title}" eager />
    </div>
    <div class="info">
      <p class="kind">{kind}</p>
      <h1>{album.title}</h1>
      <p class="meta">
        {#if album.artistId}
          <a class="artist" href="/artist/{album.artistId}">{album.artistName}</a>
        {:else}
          <span class="artist">{album.artistName}</span>
        {/if}
        {#if yearOf(album.firstReleaseDate)}
          <span aria-hidden="true">·</span>
          <span>{yearOf(album.firstReleaseDate)}</span>
        {/if}
        {#if album.tracks.length > 0}
          <span aria-hidden="true">·</span>
          <span>
            {album.tracks.length}
            {album.tracks.length > 1 ? 'titres' : 'titre'}{#if totalMs > 0}, {formatTotal(totalMs)}{/if}
          </span>
        {/if}
      </p>
    </div>
  </header>

  {#if album.tracks.length === 0}
    <p class="empty">La liste des titres de cet album n'est pas encore connue.</p>
  {:else}
    <div class="tracklist">
      <div class="row head" aria-hidden="true">
        <span class="num">#</span>
        <span>Titre</span>
        <span class="duration">
          <svg viewBox="0 0 24 24" width="16" height="16">
            <path
              fill="currentColor"
              d="M12 2a10 10 0 1 1 0 20 10 10 0 0 1 0-20Zm0 2a8 8 0 1 0 0 16 8 8 0 0 0 0-16Zm1 3v4.59l3.2 3.2-1.4 1.42L11 12.4V7h2Z"
            />
          </svg>
        </span>
      </div>
      {#each discs as { disc, tracks } (disc)}
        {#if discs.length > 1}
          <h2 class="disc">Disque {disc}</h2>
        {/if}
        <ol class="tracks" aria-label={discs.length > 1 ? `Disque ${disc}` : 'Titres'}>
          {#each tracks as track, i (track.recordingId + ':' + i)}
            <li class="row">
              <span class="num">{track.position}</span>
              <span class="title">
                <span class="title-text">{track.title}</span>
                <span class="title-artist">{album.artistName}</span>
              </span>
              <span class="duration">
                {#if track.lengthMs > 0}
                  <span class="visually-hidden">Durée </span>{formatDuration(track.lengthMs)}
                {/if}
              </span>
            </li>
          {/each}
        </ol>
      {/each}
    </div>
  {/if}
{:else}
  <Status {loading} {error} />
  {#if loading}
    <div class="header skeleton" aria-hidden="true">
      <div class="art"><span class="block"></span></div>
      <div class="info">
        <span class="bar short"></span>
        <span class="bar title"></span>
        <span class="bar"></span>
      </div>
    </div>
  {/if}
{/if}

<style>
  .header {
    display: flex;
    align-items: flex-end;
    gap: 32px;
    padding: 8px 0 32px;
  }

  .art {
    flex: none;
    width: clamp(200px, 34vw, 500px);
  }

  .info {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .kind {
    font-size: 0.875rem;
    font-weight: 700;
  }

  h1 {
    font-size: clamp(1.75rem, 4.5vw, 4rem);
    line-height: 1.05;
    font-weight: 900;
    letter-spacing: -0.035em;
    overflow-wrap: anywhere;
  }

  .meta {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    color: var(--text-muted);
    font-size: 0.95rem;
  }

  .artist {
    color: var(--text);
    font-weight: 700;
  }

  .empty {
    color: var(--text-muted);
  }

  .tracklist {
    max-width: 1100px;
  }

  .tracks {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .row {
    display: grid;
    grid-template-columns: 40px minmax(0, 1fr) 64px;
    align-items: center;
    gap: 16px;
    padding: 8px 16px;
    border-radius: var(--radius);
  }

  li.row:hover {
    background: var(--surface);
  }

  .head {
    margin-bottom: 8px;
    border-bottom: 1px solid var(--border);
    border-radius: 0;
    color: var(--text-muted);
    font-size: 0.875rem;
  }

  .num {
    text-align: right;
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
  }

  .title {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .title-text,
  .title-artist {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .title-text {
    font-weight: 500;
  }

  .title-artist {
    font-size: 0.875rem;
    color: var(--text-muted);
  }

  .duration {
    display: flex;
    justify-content: flex-end;
    color: var(--text-muted);
    font-size: 0.875rem;
    font-variant-numeric: tabular-nums;
  }

  .disc {
    margin: 24px 0 8px;
    padding: 0 16px;
    font-size: 1rem;
    font-weight: 700;
    color: var(--text-muted);
  }

  .disc:first-of-type {
    margin-top: 8px;
  }

  .block,
  .bar {
    display: block;
    background: var(--surface);
    animation: pulse 1.4s ease-in-out infinite;
  }

  .block {
    aspect-ratio: 1;
    border-radius: var(--radius);
  }

  .bar {
    width: 240px;
    max-width: 50vw;
    height: 14px;
    border-radius: 4px;
  }

  .bar.short {
    width: 80px;
  }

  .bar.title {
    width: 360px;
    height: 44px;
  }

  @keyframes pulse {
    50% {
      opacity: 0.5;
    }
  }

  @media (max-width: 760px) {
    .header {
      flex-direction: column;
      align-items: stretch;
      gap: 20px;
      padding-bottom: 20px;
    }

    .art {
      width: min(280px, 72vw);
      align-self: center;
    }

    .row {
      grid-template-columns: 28px minmax(0, 1fr) auto;
      gap: 12px;
      padding: 8px;
    }

    .disc {
      padding: 0 8px;
    }
  }
</style>
