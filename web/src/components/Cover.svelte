<script lang="ts">
  import { coverUrl } from '../lib/api';
  import placeholder from '../assets/cover-placeholder.svg';

  interface Props {
    albumId: string;
    size: 250 | 500;
    alt: string;
    eager?: boolean;
  }

  let { albumId, size, alt, eager = false }: Props = $props();

  // Remember which album failed, so a new albumId retries its own cover.
  let failedId = $state<string | null>(null);
  const src = $derived(failedId === albumId || !albumId ? placeholder : coverUrl(albumId, size));
</script>

<div class="cover">
  <img
    {src}
    {alt}
    width={size}
    height={size}
    loading={eager ? 'eager' : 'lazy'}
    decoding="async"
    onerror={() => (failedId = albumId)}
  />
</div>

<style>
  .cover {
    position: relative;
    aspect-ratio: 1;
    width: 100%;
    overflow: hidden;
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: 0 8px 24px rgb(0 0 0 / 0.45);
  }

  img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
</style>
