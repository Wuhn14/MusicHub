<script lang="ts">
  import type { AlbumSummary } from '../lib/api';
  import { albumMeta } from '../lib/format';
  import Cover from './Cover.svelte';

  interface Props {
    album: AlbumSummary;
    /** Show the artist name in the subtitle (hidden on the artist page). */
    showArtist?: boolean;
    /** Overrides the link target (default: the catalog album page). */
    href?: string;
    /** Overrides the id used for the cover (empty shows the placeholder). */
    coverId?: string;
  }

  let { album, showArtist = true, href, coverId }: Props = $props();

  const meta = $derived(albumMeta(album.firstReleaseDate, album.primaryType, album.secondaryTypes));
</script>

<li class="card">
  <a class="link" href={href ?? `/album/${album.id}`}>
    <Cover albumId={coverId ?? album.id} size={250} alt="Pochette de {album.title}" />
    <span class="title" title={album.title}>{album.title}</span>
  </a>
  <p class="sub">
    {#if showArtist && album.artistName}
      {#if album.artistId}
        <a href="/artist/{album.artistId}">{album.artistName}</a>
      {:else}
        {album.artistName}
      {/if}
      {#if meta}<span aria-hidden="true"> · </span>{/if}
    {/if}
    {meta}
  </p>
</li>

<style>
  .card {
    position: relative;
    padding: 12px;
    border-radius: var(--radius-lg);
    background: var(--bg-elevated);
    transition: background-color 0.2s ease;
    min-width: 0;
  }

  .card:hover,
  .card:focus-within {
    background: var(--surface-hover);
  }

  .link {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .link:hover {
    text-decoration: none;
  }

  .link:hover .title {
    text-decoration: underline;
  }

  .title {
    display: block;
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .sub {
    margin-top: 2px;
    font-size: 0.875rem;
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .sub a:hover {
    color: var(--text);
  }
</style>
