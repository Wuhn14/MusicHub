<script lang="ts">
  import { parseRoute, startRouter, type Route } from './lib/router';
  import Layout from './components/Layout.svelte';
  import Home from './routes/Home.svelte';
  import Search from './routes/Search.svelte';
  import Artist from './routes/Artist.svelte';
  import Settings from './routes/Settings.svelte';
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

  const active = $derived(route.name === 'home' ? 'home' : route.name === 'search' ? 'search' : route.name === 'settings' ? 'settings' : null);
</script>

<Layout {active} pageKey={String(visit)}>
  {#key visit}
    {#if route.name === 'home'}
      <Home />
    {:else if route.name === 'search'}
      <Search initialQuery={route.query} />
    {:else if route.name === 'settings'}
      <Settings />
    {:else if route.name === 'artist'}
      <Artist id={route.id} />
    {:else if route.name === 'album'}
      <Album id={route.id} />
    {:else}
      <NotFound />
    {/if}
  {/key}
</Layout>

