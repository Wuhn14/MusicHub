<script lang="ts">
  import type { AlbumSummary } from '../lib/api';
  import { albumMeta } from '../lib/format';
  import Cover from './Cover.svelte';
  import Icon from './Icon.svelte';

  interface Props {
    album: AlbumSummary;
    /** Show the artist name in the subtitle (hidden on the artist page). */
    showArtist?: boolean;
    /** Overrides the link target (default: the catalog album page). */
    href?: string;
    /** Overrides the id used for the cover (empty shows the placeholder). */
    coverId?: string;
    /** When set, a play button is shown over the cover. */
    onplay?: () => void;
  }

  let { album, showArtist = true, href, coverId, onplay }: Props = $props();

  const meta = $derived(albumMeta(album.firstReleaseDate, album.primaryType, album.secondaryTypes));
</script>

<li class="card">
  <a class="link" href={href ?? `/album/${album.id}`}>
    <Cover albumId={coverId ?? album.id} size={250} alt="Pochette de {album.title}" />
    <span class="title" title={album.title}>{album.title}</span>
  </a>
  {#if onplay}
    <!-- Overlay matching the square cover, so the button sits on its corner. -->
    <div class="over-cover">
      <button type="button" class="play" aria-label="Lire {album.title}" onclick={onplay}>
        <Icon name="play" size={22} />
      </button>
    </div>
  {/if}
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

  /* Play button over the bottom-right corner of the cover. */
  .over-cover {
    position: absolute;
    top: 12px;
    left: 12px;
    right: 12px;
    aspect-ratio: 1;
    pointer-events: none;
  }

  .play {
    position: absolute;
    right: 8px;
    bottom: 8px;
    pointer-events: auto;
    display: grid;
    place-items: center;
    width: 46px;
    height: 46px;
    padding: 0;
    border: 0;
    border-radius: 50%;
    background: var(--accent);
    color: var(--on-accent);
    box-shadow: 0 8px 16px rgb(0 0 0 / 0.45);
    cursor: pointer;
    opacity: 0;
    transform: translateY(8px);
    transition: opacity 0.2s ease, transform 0.2s ease, background-color 0.15s ease;
  }

  .card:hover .play,
  .card:focus-within .play {
    opacity: 1;
    transform: none;
  }

  .play:hover {
    background: var(--accent-strong);
    transform: scale(1.05) !important;
  }

  @media (hover: none) {
    .play {
      opacity: 1;
      transform: none;
      width: 40px;
      height: 40px;
    }
  }
</style>
