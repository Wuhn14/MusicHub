<script lang="ts">
  import type { AlbumSummary } from '../lib/api';
  import AlbumCard from './AlbumCard.svelte';

  interface Props {
    albums: AlbumSummary[];
    showArtist?: boolean;
  }

  let { albums, showArtist = true }: Props = $props();
</script>

<ul class="grid">
  {#each albums as album (album.id)}
    <AlbumCard {album} {showArtist} />
  {/each}
</ul>

<style>
  .grid {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
    gap: 16px;
  }

  @media (max-width: 760px) {
    .grid {
      grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
      gap: 10px;
    }
  }
</style>
