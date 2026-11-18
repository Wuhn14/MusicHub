<script lang="ts">
  import { errorMessage, getArtist, isAbort, type AlbumSummary, type ArtistPage } from '../lib/api';
  import { countryName } from '../lib/format';
  import AlbumGrid from '../components/AlbumGrid.svelte';
  import Status from '../components/Status.svelte';

  interface Props {
    id: string;
  }

  let { id }: Props = $props();

  let page = $state<ArtistPage | null>(null);
  let loading = $state(true);
  let error = $state('');

  $effect(() => {
    const controller = new AbortController();
    page = null;
    error = '';
    loading = true;
    getArtist(id, controller.signal)
      .then((res) => {
        page = res;
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

  const groupOrder = [
    { type: 'Album', label: 'Albums' },
    { type: 'EP', label: 'EP' },
    { type: 'Single', label: 'Singles' },
    { type: '', label: 'Autres' },
  ];

  // Newest first; albums without a release date go last.
  function byDateDesc(a: AlbumSummary, b: AlbumSummary): number {
    if (!a.firstReleaseDate || !b.firstReleaseDate) {
      return a.firstReleaseDate ? -1 : b.firstReleaseDate ? 1 : 0;
    }
    return b.firstReleaseDate.localeCompare(a.firstReleaseDate);
  }

  const groups = $derived.by(() => {
    const albums = page?.albums ?? [];
    const known = new Set(['Album', 'EP', 'Single']);
    return groupOrder
      .map(({ type, label }) => ({
        label,
        albums: albums
          .filter((a) => (type ? a.primaryType === type : !known.has(a.primaryType)))
          .sort(byDateDesc),
      }))
      .filter((g) => g.albums.length > 0);
  });

  const subtitle = $derived(
    page
      ? [page.artist.disambiguation, countryName(page.artist.country)].filter(Boolean).join(' · ')
      : '',
  );

  function initial(name: string): string {
    return name.trim().charAt(0).toUpperCase() || '?';
  }
</script>

<svelte:head>
  <title>{page ? page.artist.name : 'Artiste'} · MusicHub</title>
</svelte:head>

{#if page}
  <header class="header">
    <span class="avatar" aria-hidden="true">{initial(page.artist.name)}</span>
    <div class="info">
      <p class="kind">Artiste</p>
      <h1>{page.artist.name}</h1>
      <p class="sub">
        {#if subtitle}{subtitle} · {/if}{page.albums.length}
        {page.albums.length > 1 ? 'parutions' : 'parution'}
      </p>
    </div>
  </header>

  {#if groups.length === 0}
    <p class="empty">Aucune parution connue pour cet artiste.</p>
  {/if}

  {#each groups as group (group.label)}
    <section aria-label={group.label}>
      <h2>{group.label}</h2>
      <AlbumGrid albums={group.albums} showArtist={false} />
    </section>
  {/each}
{:else}
  <Status {loading} {error} />
  {#if loading}
    <div class="header skeleton" aria-hidden="true">
      <span class="avatar"></span>
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
    gap: 28px;
    padding: 16px 0 32px;
  }

  .avatar {
    flex: none;
    display: grid;
    place-items: center;
    width: clamp(120px, 18vw, 200px);
    aspect-ratio: 1;
    border-radius: 50%;
    background: radial-gradient(circle at 30% 25%, #3a3a3a, #1c1c1c);
    color: var(--accent);
    font-size: clamp(3rem, 7vw, 5rem);
    font-weight: 800;
    box-shadow: 0 16px 40px rgb(0 0 0 / 0.5);
  }

  .info {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .kind {
    font-size: 0.875rem;
    font-weight: 700;
  }

  h1 {
    font-size: clamp(2rem, 6vw, 4.5rem);
    line-height: 1.05;
    font-weight: 900;
    letter-spacing: -0.04em;
    overflow-wrap: anywhere;
  }

  .sub {
    color: var(--text-muted);
  }

  section {
    margin-top: 32px;
  }

  h2 {
    margin-bottom: 16px;
    font-size: 1.5rem;
    font-weight: 800;
    letter-spacing: -0.02em;
  }

  .empty {
    color: var(--text-muted);
  }

  .skeleton .avatar,
  .bar {
    background: var(--surface);
    animation: pulse 1.4s ease-in-out infinite;
  }

  .bar {
    display: block;
    width: 240px;
    max-width: 60vw;
    height: 14px;
    border-radius: 4px;
  }

  .bar.short {
    width: 80px;
  }

  .bar.title {
    width: 360px;
    height: 48px;
  }

  @keyframes pulse {
    50% {
      opacity: 0.5;
    }
  }

  @media (max-width: 760px) {
    .header {
      flex-direction: column;
      align-items: center;
      text-align: center;
      gap: 16px;
      padding-bottom: 16px;
    }

    .info {
      align-items: center;
    }
  }
</style>
