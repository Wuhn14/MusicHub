<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { LibTrack } from '../lib/api';
  import { formatDuration } from '../lib/format';

  interface Props {
    tracks: LibTrack[];
    /** Show the album name under the title (flat lists such as "Titres"). */
    showAlbum?: boolean;
    /**
     * Slot rendered at the start of every row. The play buttons will go
     * here; nothing is rendered while it is not provided.
     */
    leading?: Snippet<[LibTrack]>;
  }

  let { tracks, showAlbum = false, leading }: Props = $props();
</script>

<div class="tracklist">
  <div class="row head" class:with-lead={!!leading} aria-hidden="true">
    {#if leading}<span></span>{/if}
    <span class="num">#</span>
    <span>Titre</span>
    <span class="artist">Artiste</span>
    <span class="duration">Durée</span>
  </div>
  <ol class="tracks" aria-label="Titres">
    {#each tracks as track, i (track.fileId)}
      <li class="row" class:with-lead={!!leading}>
        {#if leading}{@render leading(track)}{/if}
        <span class="num">{track.position > 0 ? track.position : i + 1}</span>
        <span class="title">
          <span class="title-text">
            {track.title}
            {#if track.local}<span class="badge" title="Titre de ta bibliothèque locale">local</span>{/if}
          </span>
          <span class="title-sub">{showAlbum ? `${track.artist} · ${track.album}` : track.artist}</span>
        </span>
        <span class="artist">{track.artist}</span>
        <span class="duration">
          {#if track.durationMs > 0}
            <span class="visually-hidden">Durée </span>{formatDuration(track.durationMs)}
          {/if}
        </span>
      </li>
    {/each}
  </ol>
</div>

<style>
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
    grid-template-columns: 40px minmax(0, 1fr) minmax(0, 0.7fr) 64px;
    align-items: center;
    gap: 16px;
    padding: 8px 16px;
    border-radius: var(--radius);
  }

  .row.with-lead {
    grid-template-columns: 40px 40px minmax(0, 1fr) minmax(0, 0.7fr) 64px;
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
  .title-sub,
  .artist {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .title-text {
    font-weight: 500;
  }

  .title-sub {
    display: none;
    font-size: 0.875rem;
    color: var(--text-muted);
  }

  .artist {
    color: var(--text-muted);
    font-size: 0.875rem;
  }

  .duration {
    text-align: right;
    color: var(--text-muted);
    font-size: 0.875rem;
    font-variant-numeric: tabular-nums;
  }

  .badge {
    display: inline-block;
    margin-left: 6px;
    padding: 1px 7px;
    border: 1px solid var(--accent);
    border-radius: 999px;
    color: var(--accent);
    font-size: 0.7rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    vertical-align: 1px;
  }

  @media (max-width: 760px) {
    .row {
      grid-template-columns: 28px minmax(0, 1fr) auto;
      gap: 12px;
      padding: 8px;
    }

    .row.with-lead {
      grid-template-columns: 36px 28px minmax(0, 1fr) auto;
    }

    .artist {
      display: none;
    }

    .title-sub {
      display: block;
    }
  }
</style>
