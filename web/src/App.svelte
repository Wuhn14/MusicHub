<script lang="ts">
  import { parseRoute, startRouter, type Route } from './lib/router';
  import Layout from './components/Layout.svelte';
  import Home from './routes/Home.svelte';
  import Search from './routes/Search.svelte';
  import Artist from './routes/Artist.svelte';
  import Settings from './routes/Settings.svelte';
  import Upload from './routes/Upload.svelte';
  import ToIdentify from './routes/ToIdentify.svelte';
  import Library from './routes/Library.svelte';
  import LibraryAlbum from './routes/LibraryAlbum.svelte';
  import Album from './routes/Album.svelte';
  import NotFound from './routes/NotFound.svelte';

  let route = $state<Route>(parseRoute(location));
  // Bumped on every navigation so pages remount and scroll resets.
  let visit = $state(0);

  $effect(() =>
    startRouter(() => {
      route = parseRoute(location);
      visit += 1;
    }),
  );

  const active = $derived.by(() => {
    switch (route.name) {
      case 'home':
      case 'search':
      case 'settings':
      case 'upload':
      case 'identify':
        return route.name;
      case 'library':
      case 'libraryAlbum':
        return 'library';
      default:
        return null;
    }
  });
</script>

<Layout {active} pageKey={String(visit)}>
  {#key visit}
    {#if route.name === 'home'}
      <Home />
    {:else if route.name === 'search'}
      <Search initialQuery={route.query} />
    {:else if route.name === 'settings'}
      <Settings />
    {:else if route.name === 'upload'}
      <Upload />
    {:else if route.name === 'identify'}
      <ToIdentify />
    {:else if route.name === 'library'}
      <Library tab={route.tab} sort={route.sort} q={route.q} artist={route.artist} />
    {:else if route.name === 'libraryAlbum'}
      <LibraryAlbum key={route.key} />
    {:else if route.name === 'artist'}
      <Artist id={route.id} />
    {:else if route.name === 'album'}
      <Album id={route.id} />
    {:else}
      <NotFound />
    {/if}
  {/key}
</Layout>

