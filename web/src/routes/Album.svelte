<script lang="ts">
  import { errorMessage, getAlbum, getAlbumAvailability, isAbort, type Album, type Track } from '../lib/api';
  import { formatDuration, formatTotal, typeLabel, yearOf } from '../lib/format';
  import { player, type PlayableTrack } from '../lib/player.svelte';
  import Cover from '../components/Cover.svelte';
  import Icon from '../components/Icon.svelte';
  import Status from '../components/Status.svelte';

  interface Props {
    id: string;
  }

  let { id }: Props = $props();

  let album = $state<Album | null>(null);
  let loading = $state(true);
  let error = $state('');
  /** Recording id → library file id, for the tracks the library can play. */
  let available = $state<Record<string, string>>({});

  $effect(() => {
    const controller = new AbortController();
    album = null;
    error = '';
    loading = true;
    available = {};
    // Availability is a bonus: when it fails the album still shows, greyed.
    getAlbumAvailability(id, controller.signal)
      .then((res) => {
        available = res.tracks;
      })
      .catch(() => {});
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

  /** The playable tracks, in album order: the queue built by a click. */
  const playableTracks = $derived(discs.flatMap((d) => d.tracks).filter((t) => available[t.recordingId]));

  const currentId = $derived(player.current?.track.fileId ?? '');

  function toPlayable(t: Track): PlayableTrack {
    return {
      fileId: available[t.recordingId],
      title: t.title,
      artist: album?.artistName ?? '',
      album: album?.title ?? '',
      coverUrl: album ? `/api/covers/${encodeURIComponent(album.id)}` : '',
      durationMs: t.lengthMs > 0 ? t.lengthMs : 0,
    };
  }

  /** Plays the album's playable tracks from t (from the first when null). */
  function play(t: Track | null): void {
    if (t && available[t.recordingId] === currentId) {
      player.toggle();
      return;
    }
    const start = t ? playableTracks.indexOf(t) : 0;
    player.playList(playableTracks.map(toPlayable), Math.max(0, start));
  }

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
      {#if playableTracks.length > 0}
        <div class="actions">
          <button type="button" class="play-all" onclick={() => play(null)}>
            <Icon name="play" size={20} />
            Lire
          </button>
          <span class="owned">
            {playableTracks.length === album.tracks.length
              ? 'Dans ta bibliothèque'
              : `${playableTracks.length} sur ${album.tracks.length} dans ta bibliothèque`}
          </span>
        </div>
      {/if}
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
            {@const fileId = available[track.recordingId]}
            {#if fileId}
              {@const current = fileId === currentId}
              <li class="row available" class:current aria-current={current ? 'true' : undefined}>
                <button
                  type="button"
                  class="main"
                  aria-label={current
                    ? `${player.playing ? 'Mettre en pause' : 'Reprendre'} ${track.title}`
                    : `Lire ${track.title}`}
                  onclick={() => play(track)}
                >
                  <span class="num">
                    <span class="n">{track.position}</span>
                    <span class="play-icon" aria-hidden="true">
                      <Icon name={current && player.playing ? 'pause' : 'play'} size={16} />
                    </span>
                  </span>
                  <span class="title">
                    <span class="title-text">{track.title}</span>
                    <span class="title-artist">{album.artistName}</span>
                  </span>
                  <span class="duration">
                    {#if track.lengthMs > 0}
                      <span class="visually-hidden">Durée </span>{formatDuration(track.lengthMs)}
                    {/if}
                  </span>
                </button>
              </li>
            {:else}
              <li class="row unavailable">
                <div class="main" role="group" aria-disabled="true" title="Pas encore dans ta bibliothèque">
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
                  <span class="visually-hidden">(pas encore dans ta bibliothèque)</span>
                </div>
              </li>
            {/if}
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

  li.row:hover,
  li.row:focus-within {
    background: var(--surface);
  }

  /* The row content sits in .main: a button when the track is playable. */
  li.available,
  li.unavailable {
    padding: 0;
  }

  .main {
    grid-column: 1 / -1;
    display: grid;
    grid-template-columns: subgrid;
    align-items: center;
    padding: 8px 16px;
    border: 0;
    border-radius: var(--radius);
    background: none;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .num {
    position: relative;
  }

  .play-icon {
    position: absolute;
    right: 0;
    top: 50%;
    transform: translateY(-50%);
    display: none;
    color: var(--text);
  }

  li.available:hover .n,
  .main:focus-visible .n,
  .current .n {
    visibility: hidden;
  }

  li.available:hover .play-icon,
  .main:focus-visible .play-icon,
  .current .play-icon {
    display: block;
  }

  .current .title-text,
  .current .play-icon {
    color: var(--accent);
  }

  /* Not in the library yet: shown, but greyed and inert. */
  li.unavailable {
    opacity: 0.45;
  }

  li.unavailable .main {
    cursor: default;
  }

  li.unavailable:hover {
    background: none;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 16px;
    margin-top: 6px;
  }

  .play-all {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 10px 22px;
    border: 0;
    border-radius: 999px;
    background: var(--accent);
    color: var(--on-accent);
    font: inherit;
    font-weight: 700;
    cursor: pointer;
  }

  .play-all:hover {
    background: var(--accent-strong);
  }

  .owned {
    color: var(--text-muted);
    font-size: 0.875rem;
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
