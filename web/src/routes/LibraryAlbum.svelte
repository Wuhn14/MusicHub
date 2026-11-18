<script lang="ts">
  import { errorMessage, getLibraryAlbum, isAbort, type LibAlbumPage } from '../lib/api';
  import { formatTotal } from '../lib/format';
  import Cover from '../components/Cover.svelte';
  import Status from '../components/Status.svelte';
  import TrackList from '../components/TrackList.svelte';

  interface Props {
    key: string;
  }

  let { key }: Props = $props();

  let album = $state<LibAlbumPage | null>(null);
  let loading = $state(true);
  let error = $state('');

  $effect(() => {
    const controller = new AbortController();
    album = null;
    error = '';
    loading = true;
    getLibraryAlbum(key, controller.signal)
      .then((res) => {
        album = res;
        loading = false;
      })
      .catch((err: unknown) => {
        if (isAbort(err)) return;
        error = errorMessage(err);
        loading = false;
      });
    return () => controller.abort();
  });
</script>

<svelte:head>
  <title>{album ? `${album.title} · ${album.artist}` : 'Album'} · MusicHub</title>
</svelte:head>

<p class="back"><a href="/library">← Bibliothèque</a></p>

{#if album}
  <header class="header">
    <div class="art">
      <Cover albumId={album.albumId} size={500} alt="Pochette de {album.title}" eager />
    </div>
    <div class="info">
      <p class="kind">Album de ta bibliothèque</p>
      <h1>{album.title}</h1>
      <p class="meta">
        <a href="/library?tab=albums&artist={encodeURIComponent(album.artist)}">{album.artist}</a>
        {#if album.year}<span aria-hidden="true">·</span><span>{album.year}</span>{/if}
        <span aria-hidden="true">·</span>
        <span>
          {album.trackCount}
          {album.trackCount > 1 ? 'titres' : 'titre'}{#if album.durationMs > 0}, {formatTotal(album.durationMs)}{/if}
        </span>
      </p>
      <!-- Play controls (play album, shuffle) will go here. -->
    </div>
  </header>
  <TrackList tracks={album.tracks} />
{:else}
  <Status {loading} {error} />
{/if}

<style>
  .back {
    margin-bottom: 8px;
    font-size: 0.9rem;
    color: var(--text-muted);
  }

  .header {
    display: flex;
    align-items: flex-end;
    gap: 32px;
    padding: 8px 0 32px;
  }

  .art {
    flex: none;
    width: clamp(200px, 34vw, 400px);
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
    font-size: clamp(2rem, 5vw, 4rem);
    font-weight: 800;
    line-height: 1.05;
    letter-spacing: -0.03em;
    overflow-wrap: anywhere;
  }

  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    color: var(--text-muted);
  }

  .meta a {
    color: var(--text);
    font-weight: 700;
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
  }
</style>
