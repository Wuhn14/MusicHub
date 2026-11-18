<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    /** Navigation entry to highlight, if any. */
    active: 'home' | 'search' | 'settings' | null;
    /** Changes on every navigation; used to reset the scroll position. */
    pageKey: string;
    children: Snippet;
  }

  let { active, pageKey, children }: Props = $props();

  let main = $state<HTMLElement>();

  $effect(() => {
    void pageKey;
    main?.scrollTo(0, 0);
  });
</script>

<div class="shell">
  <a class="skip" href="#contenu">Aller au contenu</a>

  <aside class="sidebar">
    <a class="brand" href="/" aria-label="MusicHub, accueil">
      <span class="logo" aria-hidden="true"></span>
      <span>MusicHub</span>
    </a>
    <nav aria-label="Navigation principale">
      <ul>
        <li>
          <a href="/" class:active={active === 'home'} aria-current={active === 'home' ? 'page' : undefined}>
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path fill="currentColor" d="M12 3 2 11.5h3V21h5.5v-6h3v6H19v-9.5h3L12 3Z" />
            </svg>
            <span>Accueil</span>
          </a>
        </li>
        <li>
          <a
            href="/search"
            class:active={active === 'search'}
            aria-current={active === 'search' ? 'page' : undefined}
          >
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                fill="currentColor"
                d="M10.5 3a7.5 7.5 0 0 1 5.96 12.05l4.74 4.74-1.41 1.41-4.74-4.74A7.5 7.5 0 1 1 10.5 3Zm0 2a5.5 5.5 0 1 0 0 11 5.5 5.5 0 0 0 0-11Z"
              />
            </svg>
            <span>Recherche</span>
          </a>
        </li>
        <li>
          <a
            href="/settings"
            class:active={active === 'settings'}
            aria-current={active === 'settings' ? 'page' : undefined}
          >
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                fill="currentColor"
                d="M19.43 12.98c.04-.32.07-.64.07-.98s-.03-.66-.07-.98l2.11-1.65a.5.5 0 0 0 .12-.64l-2-3.46a.5.5 0 0 0-.61-.22l-2.49 1a7.3 7.3 0 0 0-1.69-.98l-.38-2.65A.49.49 0 0 0 14 2h-4a.49.49 0 0 0-.49.42l-.38 2.65c-.61.25-1.17.59-1.69.98l-2.49-1a.5.5 0 0 0-.61.22l-2 3.46a.5.5 0 0 0 .12.64l2.11 1.65c-.04.32-.07.65-.07.98s.03.66.07.98l-2.11 1.65a.5.5 0 0 0-.12.64l2 3.46c.13.22.39.3.61.22l2.49-1c.52.4 1.08.73 1.69.98l.38 2.65c.03.24.24.42.49.42h4c.25 0 .46-.18.49-.42l.38-2.65c.61-.25 1.17-.59 1.69-.98l2.49 1c.23.09.49 0 .61-.22l2-3.46a.5.5 0 0 0-.12-.64l-2.11-1.65ZM12 15.5A3.5 3.5 0 1 1 12 8.5a3.5 3.5 0 0 1 0 7Z"
              />
            </svg>
            <span>Réglages</span>
          </a>
        </li>
      </ul>
    </nav>
  </aside>

  <main id="contenu" class="main" bind:this={main} tabindex="-1">
    <div class="mobile-brand">
      <span class="logo" aria-hidden="true"></span>
      <span>MusicHub</span>
    </div>
    <div class="page">
      {@render children()}
    </div>
  </main>

  <footer class="player" aria-label="Lecteur">
    <div class="now">
      <div class="art" aria-hidden="true">
        <svg viewBox="0 0 24 24">
          <path fill="currentColor" d="M12 3v10.55A4 4 0 1 0 14 17V7h4V3h-6Z" />
        </svg>
      </div>
      <div class="now-text">
        <p class="now-title">Aucune lecture en cours</p>
        <p class="now-sub">Choisis un album pour commencer</p>
      </div>
    </div>
    <div class="controls" aria-hidden="true">
      <span class="ctrl">
        <svg viewBox="0 0 24 24"><path fill="currentColor" d="M6 6h2v12H6V6Zm3.5 6 8.5 6V6l-8.5 6Z" /></svg>
      </span>
      <span class="ctrl play">
        <svg viewBox="0 0 24 24"><path fill="currentColor" d="M8 5v14l11-7L8 5Z" /></svg>
      </span>
      <span class="ctrl">
        <svg viewBox="0 0 24 24"><path fill="currentColor" d="M16 6h2v12h-2V6ZM6 18l8.5-6L6 6v12Z" /></svg>
      </span>
    </div>
    <div class="spacer"></div>
  </footer>
</div>

<style>
  .shell {
    display: grid;
    grid-template-columns: var(--sidebar-width) minmax(0, 1fr);
    grid-template-rows: minmax(0, 1fr) var(--player-height);
    grid-template-areas:
      'sidebar main'
      'player player';
    height: 100vh;
    height: 100dvh;
    gap: 8px;
    padding: 8px 8px 0;
    background: var(--bg-sidebar);
  }

  .skip {
    position: absolute;
    left: 12px;
    top: -60px;
    z-index: 10;
    padding: 8px 14px;
    border-radius: var(--radius);
    background: var(--accent);
    color: var(--on-accent);
    font-weight: 600;
  }

  .skip:focus {
    top: 12px;
  }

  .sidebar {
    grid-area: sidebar;
    display: flex;
    flex-direction: column;
    gap: 20px;
    padding: 20px 12px;
    border-radius: var(--radius-lg);
    background: var(--bg);
  }

  .brand,
  .mobile-brand {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 0 12px;
    font-size: 1.35rem;
    font-weight: 800;
    letter-spacing: -0.02em;
  }

  .brand:hover {
    text-decoration: none;
  }

  .logo {
    flex: none;
    width: 28px;
    height: 28px;
    border-radius: 50%;
    background:
      radial-gradient(circle, var(--bg) 0 3px, transparent 3.5px),
      radial-gradient(circle, transparent 0 8px, rgb(0 0 0 / 0.25) 8.5px 9.5px, transparent 10px),
      var(--accent);
  }

  nav ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  nav a {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 10px 12px;
    border-radius: var(--radius);
    color: var(--text-muted);
    font-weight: 700;
    transition: color 0.15s ease, background-color 0.15s ease;
  }

  nav a:hover {
    color: var(--text);
    text-decoration: none;
    background: var(--surface);
  }

  nav a.active {
    color: var(--text);
    background: var(--surface);
  }

  nav a.active svg {
    color: var(--accent);
  }

  nav svg {
    width: 24px;
    height: 24px;
    flex: none;
  }

  .main {
    grid-area: main;
    overflow-y: auto;
    border-radius: var(--radius-lg);
    background: linear-gradient(180deg, #232323 0, var(--bg) 320px);
    outline: none;
  }

  .mobile-brand {
    display: none;
  }

  .page {
    max-width: 1400px;
    padding: 32px;
  }

  .player {
    grid-area: player;
    display: grid;
    grid-template-columns: 1fr auto 1fr;
    align-items: center;
    gap: 16px;
    padding: 0 16px;
    background: var(--bg-sidebar);
  }

  .now {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }

  .art {
    flex: none;
    display: grid;
    place-items: center;
    width: 48px;
    height: 48px;
    border-radius: 6px;
    background: var(--surface);
    color: var(--text-faint);
  }

  .art svg {
    width: 22px;
    height: 22px;
  }

  .now-text {
    min-width: 0;
  }

  .now-title,
  .now-sub {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .now-title {
    font-size: 0.875rem;
    font-weight: 600;
    color: var(--text-muted);
  }

  .now-sub {
    font-size: 0.75rem;
    color: var(--text-faint);
  }

  .controls {
    display: flex;
    align-items: center;
    gap: 16px;
    color: var(--text-faint);
  }

  .ctrl {
    display: grid;
    place-items: center;
    width: 32px;
    height: 32px;
  }

  .ctrl svg {
    width: 20px;
    height: 20px;
  }

  .ctrl.play {
    width: 36px;
    height: 36px;
    border-radius: 50%;
    background: var(--surface-hover);
  }

  /* Narrow screens: top brand bar, content, player, then a bottom tab bar. */
  @media (max-width: 760px) {
    .shell {
      grid-template-columns: minmax(0, 1fr);
      grid-template-rows: minmax(0, 1fr) auto var(--bottom-nav-height);
      grid-template-areas:
        'main'
        'player'
        'sidebar';
      gap: 0;
      padding: 0;
    }

    .sidebar {
      padding: 0;
      border-radius: 0;
      background: var(--bg-sidebar);
      border-top: 1px solid var(--border);
    }

    .brand {
      display: none;
    }

    nav,
    nav ul {
      height: 100%;
    }

    nav ul {
      flex-direction: row;
      gap: 0;
    }

    nav li {
      flex: 1;
    }

    nav a {
      height: 100%;
      flex-direction: column;
      justify-content: center;
      gap: 2px;
      padding: 6px;
      border-radius: 0;
      font-size: 0.75rem;
      font-weight: 600;
    }

    nav a:hover,
    nav a.active {
      background: transparent;
    }

    .main {
      border-radius: 0;
    }

    .mobile-brand {
      display: flex;
      padding: 16px 16px 0;
      font-size: 1.15rem;
    }

    .mobile-brand .logo {
      width: 24px;
      height: 24px;
    }

    .page {
      padding: 16px;
    }

    .player {
      grid-template-columns: minmax(0, 1fr) auto;
      height: 64px;
      margin: 0 8px 6px;
      padding: 0 8px;
      border-radius: var(--radius);
      background: var(--surface);
    }

    .art {
      width: 40px;
      height: 40px;
    }

    .controls .ctrl:not(.play),
    .spacer {
      display: none;
    }
  }
</style>
