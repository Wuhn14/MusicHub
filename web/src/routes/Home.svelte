<script lang="ts">
  import { navigate } from '../lib/router';

  let query = $state('');

  function submit(event: SubmitEvent) {
    event.preventDefault();
    const q = query.trim();
    navigate(q ? `/search?q=${encodeURIComponent(q)}` : '/search');
  }
</script>

<svelte:head>
  <title>MusicHub</title>
</svelte:head>

<section class="hero">
  <p class="eyebrow">Bienvenue sur MusicHub</p>
  <h1>Toute la musique du monde, en un seul endroit.</h1>
  <p class="lead">
    Explore le catalogue MusicBrainz : retrouve un artiste, parcours sa discographie et ouvre
    n'importe quel album pour voir ses titres.
  </p>

  <form class="search" role="search" onsubmit={submit}>
    <label class="visually-hidden" for="home-search">Rechercher un artiste, un album ou un titre</label>
    <svg viewBox="0 0 24 24" aria-hidden="true">
      <path
        fill="currentColor"
        d="M10.5 3a7.5 7.5 0 0 1 5.96 12.05l4.74 4.74-1.41 1.41-4.74-4.74A7.5 7.5 0 1 1 10.5 3Zm0 2a5.5 5.5 0 1 0 0 11 5.5 5.5 0 0 0 0-11Z"
      />
    </svg>
    <input
      id="home-search"
      type="search"
      placeholder="Un artiste, un album, un titre…"
      autocomplete="off"
      maxlength="200"
      bind:value={query}
    />
    <button type="submit">Rechercher</button>
  </form>
</section>

<style>
  .hero {
    max-width: 760px;
    padding: 48px 0 24px;
  }

  .eyebrow {
    color: var(--accent);
    font-weight: 700;
    font-size: 0.875rem;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  h1 {
    margin-top: 12px;
    font-size: clamp(2rem, 5vw, 3.25rem);
    line-height: 1.08;
    letter-spacing: -0.03em;
    font-weight: 800;
  }

  .lead {
    margin-top: 16px;
    max-width: 60ch;
    color: var(--text-muted);
    font-size: 1.05rem;
  }

  .search {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 32px;
    padding: 6px 6px 6px 16px;
    border-radius: 999px;
    background: var(--surface);
    border: 1px solid var(--border);
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
    padding: 10px 4px;
    border: 0;
    background: transparent;
    font-size: 1rem;
  }

  input:focus-visible {
    outline: none;
  }

  button {
    flex: none;
    padding: 10px 20px;
    border: 0;
    border-radius: 999px;
    background: var(--accent);
    color: var(--on-accent);
    font-weight: 700;
    cursor: pointer;
    transition: background-color 0.15s ease, transform 0.15s ease;
  }

  button:hover {
    background: var(--accent-strong);
    transform: scale(1.03);
  }

  @media (max-width: 760px) {
    .hero {
      padding-top: 24px;
    }

    button {
      padding: 10px 14px;
    }
  }
</style>
