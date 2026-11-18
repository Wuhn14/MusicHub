<svelte:head>
  <title>Page introuvable · MusicHub</title>
</svelte:head>

<section class="not-found">
  <h1>Page introuvable</h1>
  <p>Cette adresse ne correspond à aucune page de MusicHub.</p>
  <a class="back" href="/">Retour à l'accueil</a>
</section>

<style>
  .not-found {
    padding: 48px 0;
  }

  h1 {
    font-size: 2rem;
    font-weight: 800;
    letter-spacing: -0.02em;
  }

  p {
    margin-top: 8px;
    color: var(--text-muted);
  }

  .back {
    display: inline-block;
    margin-top: 24px;
    padding: 10px 20px;
    border-radius: 999px;
    background: var(--accent);
    color: var(--on-accent);
    font-weight: 700;
  }

  .back:hover {
    text-decoration: none;
    background: var(--accent-strong);
  }
</style>
