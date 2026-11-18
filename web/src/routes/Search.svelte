<script lang="ts">
  import { untrack } from 'svelte';
  import { errorMessage, isAbort, search, type SearchResult } from '../lib/api';
  import { countryName, formatDuration } from '../lib/format';
  import { replaceQuery } from '../lib/router';
  import AlbumGrid from '../components/AlbumGrid.svelte';
  import Cover from '../components/Cover.svelte';
  import Status from '../components/Status.svelte';

  interface Props {
    initialQuery: string;
  }

  let { initialQuery }: Props = $props();

  const debounceMs = 400;

  let query = $state(untrack(() => initialQuery));
  let results = $state<SearchResult | null>(null);
  let loading = $state(false);
  let error = $state('');
  let input = $state<HTMLInputElement>();
  // A query restored from the URL is sent at once; typing is debounced.
  let immediate = untrack(() => initialQuery.trim() !== '');

  $effect(() => {
    input?.focus();
  });

  $effect(() => {
    const q = query.trim();
    replaceQuery(q);
    if (!q) {
      results = null;
      loading = false;
      error = '';
      return;
    }
    const controller = new AbortController();
    loading = true;
    error = '';
    const timer = setTimeout(async () => {
      try {
        results = await search(q, controller.signal);
        error = '';
      } catch (err) {
        if (isAbort(err)) {
          return;
        }
        results = null;
        error = errorMessage(err);
      }
      loading = false;
    }, immediate ? 0 : debounceMs);
    immediate = false;
    // Typing again cancels both the pending timer and the request in flight.
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  });

  const empty = $derived(
    results !== null &&
      results.artists.length === 0 &&
      results.albums.length === 0 &&
      results.tracks.length === 0,
  );

  function initial(name: string): string {
    return name.trim().charAt(0).toUpperCase() || '?';
  }
</script>

<svelte:head>
  <title>{query.trim() ? `${query.trim()} · Recherche` : 'Recherche'} · MusicHub</title>
</svelte:head>

<h1 class="visually-hidden">Recherche</h1>

<form class="search" role="search" onsubmit={(e) => e.preventDefault()}>
  <label class="visually-hidden" for="search-input">Rechercher un artiste, un album ou un titre</label>
  <svg viewBox="0 0 24 24" aria-hidden="true">
    <path
      fill="currentColor"
      d="M10.5 3a7.5 7.5 0 0 1 5.96 12.05l4.74 4.74-1.41 1.41-4.74-4.74A7.5 7.5 0 1 1 10.5 3Zm0 2a5.5 5.5 0 1 0 0 11 5.5 5.5 0 0 0 0-11Z"
    />
  </svg>
  <input
    id="search-input"
    type="search"
    placeholder="Que veux-tu écouter ?"
    autocomplete="off"
    spellcheck="false"
    maxlength="200"
    bind:value={query}
    bind:this={input}
  />
</form>

<div class="results" aria-busy={loading}>
  <Status {loading} {error} loadingText="Recherche en cours…" />

  {#if !query.trim()}
    <div class="hint">
      <p class="hint-title">Cherche dans tout le catalogue</p>
      <p>Tape le nom d'un artiste, d'un album ou d'un titre.</p>
    </div>
  {:else if results && !error}
    {#if empty && !loading}
      <div class="hint">
        <p class="hint-title">Aucun résultat pour « {query.trim()} »</p>
        <p>Vérifie l'orthographe ou essaie avec moins de mots.</p>
      </div>
    {/if}

    {#if results.artists.length > 0}
      <section aria-labelledby="h-artists">
        <h2 id="h-artists">Artistes</h2>
        <ul class="artists">
          {#each results.artists as artist (artist.id)}
            <li>
              <a class="artist" href="/artist/{artist.id}">
                <span class="avatar" aria-hidden="true">{initial(artist.name)}</span>
                <span class="artist-name">{artist.name}</span>
                <span class="artist-sub">
                  {[artist.disambiguation, countryName(artist.country)].filter(Boolean).join(' · ') ||
                    'Artiste'}
                </span>
              </a>
            </li>
          {/each}
        </ul>
      </section>
    {/if}

    {#if results.albums.length > 0}
      <section aria-labelledby="h-albums">
        <h2 id="h-albums">Albums</h2>
        <AlbumGrid albums={results.albums} />
      </section>
    {/if}

    {#if results.tracks.length > 0}
      <section aria-labelledby="h-tracks">
        <h2 id="h-tracks">Titres</h2>
        <ol class="tracks">
          {#each results.tracks as track, i (track.recordingId + ':' + i)}
            <li class="track">
              <div class="thumb">
                <Cover albumId={track.albumId} size={250} alt="" />
              </div>
              <div class="track-main">
                <span class="track-title">{track.title}</span>
                <span class="track-sub"
                  >{track.artistName}{#if track.albumId}<span class="sub-album"
                      ><span aria-hidden="true"> · </span><a href="/album/{track.albumId}"
                        >{track.albumTitle}</a
                      ></span
                    >{/if}</span
                >
              </div>
              <div class="track-album">
                {#if track.albumId}
                  <a href="/album/{track.albumId}">{track.albumTitle}</a>
                {:else}
                  {track.albumTitle}
                {/if}
              </div>
              <span class="duration">{formatDuration(track.lengthMs)}</span>
            </li>
          {/each}
        </ol>
      </section>
    {/if}
  {/if}
</div>

<style>
  .search {
    position: sticky;
    top: 0;
    z-index: 2;
    display: flex;
    align-items: center;
    gap: 10px;
    max-width: 560px;
    padding: 0 16px;
    border-radius: 999px;
    background: var(--surface);
    border: 1px solid var(--border);
    box-shadow: 0 8px 24px rgb(0 0 0 / 0.35);
    transition: border-color 0.15s ease;
  }

  .search:focus-within {
    border-color: var(--accent);
  }

  .search svg {
    flex: none;
    width: 22px;
    height: 22px;
    color: var(--text-muted);
  }

  input {
    flex: 1;
    min-width: 0;
    padding: 13px 0;
    border: 0;
    background: transparent;
    font-size: 1rem;
  }

  input:focus-visible {
    outline: none;
  }

  .results {
    margin-top: 24px;
  }

  section {
    margin-top: 32px;
  }

  section:first-of-type {
    margin-top: 8px;
  }

  h2 {
    margin-bottom: 16px;
    font-size: 1.5rem;
    font-weight: 800;
    letter-spacing: -0.02em;
  }

  .hint {
    padding: 48px 0;
    color: var(--text-muted);
  }

  .hint-title {
    margin-bottom: 6px;
    color: var(--text);
    font-size: 1.25rem;
    font-weight: 700;
  }

  .artists {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
    gap: 16px;
  }

  .artist {
    display: flex;
    flex-direction: column;
    gap: 4px;
    height: 100%;
    padding: 12px;
    border-radius: var(--radius-lg);
    background: var(--bg-elevated);
    transition: background-color 0.2s ease;
  }

  .artist:hover {
    text-decoration: none;
    background: var(--surface-hover);
  }

  .avatar {
    display: grid;
    place-items: center;
    aspect-ratio: 1;
    margin-bottom: 8px;
    border-radius: 50%;
    background: radial-gradient(circle at 30% 25%, #3a3a3a, #1c1c1c);
    color: var(--accent);
    font-size: clamp(2rem, 5vw, 3rem);
    font-weight: 800;
    box-shadow: 0 8px 24px rgb(0 0 0 / 0.45);
  }

  .artist-name {
    font-weight: 700;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .artist:hover .artist-name {
    text-decoration: underline;
  }

  .artist-sub {
    font-size: 0.875rem;
    color: var(--text-muted);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }

  .tracks {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .track {
    display: grid;
    grid-template-columns: 44px minmax(0, 2fr) minmax(0, 1.5fr) 56px;
    align-items: center;
    gap: 14px;
    padding: 8px;
    border-radius: var(--radius);
    transition: background-color 0.15s ease;
  }

  .track:hover,
  .track:focus-within {
    background: var(--surface);
  }

  .thumb {
    width: 44px;
  }

  .thumb :global(.cover) {
    border-radius: 4px;
    box-shadow: none;
  }

  .track-main {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .track-title,
  .track-sub,
  .track-album {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .track-title {
    font-weight: 600;
  }

  .track-sub,
  .track-album {
    font-size: 0.875rem;
    color: var(--text-muted);
  }

  .track-album a:hover,
  .sub-album a:hover {
    color: var(--text);
  }

  .sub-album {
    display: none;
  }

  .duration {
    font-size: 0.875rem;
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
    text-align: right;
  }

  @media (max-width: 760px) {
    .artists {
      grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
      gap: 10px;
    }

    .track {
      grid-template-columns: 44px minmax(0, 1fr) auto;
      gap: 12px;
    }

    .track-album {
      display: none;
    }

    .sub-album {
      display: inline;
    }
  }
</style>
