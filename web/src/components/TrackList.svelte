<script lang="ts">
  import { tick } from 'svelte';
  import type { LibTrack } from '../lib/api';
  import { formatDuration } from '../lib/format';
  import { player, toPlayable } from '../lib/player.svelte';
  import Icon from './Icon.svelte';

  interface Props {
    tracks: LibTrack[];
    /** Show the album name under the title (flat lists such as "Titres"). */
    showAlbum?: boolean;
  }

  let { tracks, showAlbum = false }: Props = $props();

  const currentId = $derived(player.current?.track.fileId ?? '');

  function play(i: number): void {
    if (tracks[i].fileId === currentId) {
      player.toggle();
      return;
    }
    player.playList(tracks.map(toPlayable), i);
  }

  // The "⋯" menu: at most one open at a time.
  let menuFor = $state<string | null>(null);
  let menuUp = $state(false);

  async function openMenu(e: MouseEvent, fileId: string): Promise<void> {
    if (menuFor === fileId) {
      menuFor = null;
      return;
    }
    const trigger = e.currentTarget as HTMLElement;
    const r = trigger.getBoundingClientRect();
    // Open upwards when there is no room below (the player bar is there).
    menuUp = window.innerHeight - r.bottom < 200;
    menuFor = fileId;
    await tick();
    trigger.parentElement?.querySelector<HTMLElement>('[role="menuitem"]')?.focus();
  }

  function closeMenu(focusTrigger: boolean): void {
    const id = menuFor;
    menuFor = null;
    if (focusTrigger && id) {
      document.querySelector<HTMLElement>(`[data-menu-trigger="${CSS.escape(id)}"]`)?.focus();
    }
  }

  function onMenuKey(e: KeyboardEvent): void {
    const items = [...(e.currentTarget as HTMLElement).querySelectorAll<HTMLElement>('[role="menuitem"]')];
    const at = items.indexOf(document.activeElement as HTMLElement);
    if (e.key === 'Escape') {
      e.preventDefault();
      closeMenu(true);
    } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      const step = e.key === 'ArrowDown' ? 1 : -1;
      items[(at + step + items.length) % items.length]?.focus();
    } else if (e.key === 'Tab') {
      closeMenu(false);
    }
  }

  function onWindowPointer(e: PointerEvent): void {
    if (menuFor && !(e.target instanceof Element && e.target.closest('.menu-wrap'))) menuFor = null;
  }

  function pick(action: 'next' | 'queue', track: LibTrack): void {
    closeMenu(true);
    if (action === 'next') player.playNext(toPlayable(track));
    else player.addToQueue(toPlayable(track));
  }
</script>

<svelte:window onpointerdown={onWindowPointer} />

<div class="tracklist">
  <div class="row head" aria-hidden="true">
    <span class="num">#</span>
    <span>Titre</span>
    <span class="artist">Artiste</span>
    <span class="duration">Durée</span>
  </div>
  <ol class="tracks" aria-label="Titres">
    {#each tracks as track, i (track.fileId)}
      {@const current = track.fileId === currentId}
      <li class="row" class:current aria-current={current ? 'true' : undefined}>
        <button
          type="button"
          class="main"
          aria-label={current ? `${player.playing ? 'Mettre en pause' : 'Reprendre'} ${track.title}` : `Lire ${track.title}`}
          onclick={() => play(i)}
        >
          <span class="num">
            {#if current}
              <span class="eq" class:paused={!player.playing} aria-hidden="true"><i></i><i></i><i></i></span>
            {:else}
              <span class="n">{track.position > 0 ? track.position : i + 1}</span>
            {/if}
            <span class="hover-icon" aria-hidden="true">
              <Icon name={current && player.playing ? 'pause' : 'play'} size={16} />
            </span>
          </span>
          <span class="title">
            <span class="title-text">
              {track.title}
              {#if track.local}<span class="badge" title="Titre de ta bibliothèque locale">local</span>{/if}
            </span>
            <span class="title-sub">{showAlbum ? `${track.artist} · ${track.album}` : track.artist}</span>
          </span>
          <span class="artist">{track.artist}</span>
          <span class="duration">
            {#if track.durationMs > 0}
              <span class="visually-hidden">Durée </span>{formatDuration(track.durationMs)}
            {/if}
          </span>
        </button>
        <div class="menu-wrap">
          <button
            type="button"
            class="more"
            class:open={menuFor === track.fileId}
            aria-label="Plus d'options pour {track.title}"
            aria-haspopup="menu"
            aria-expanded={menuFor === track.fileId}
            data-menu-trigger={track.fileId}
            onclick={(e) => openMenu(e, track.fileId)}
          >
            <Icon name="more" size={20} />
          </button>
          {#if menuFor === track.fileId}
            <div class="menu" class:up={menuUp} role="menu" tabindex="-1" aria-label="Options de {track.title}" onkeydown={onMenuKey}>
              <button type="button" role="menuitem" onclick={() => pick('next', track)}>Lire ensuite</button>
              <button type="button" role="menuitem" onclick={() => pick('queue', track)}>Ajouter à la file</button>
            </div>
          {/if}
        </div>
      </li>
    {/each}
  </ol>
</div>

<style>
  .tracklist {
    max-width: 1100px;
  }

  .tracks {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .row {
    display: grid;
    grid-template-columns: 40px minmax(0, 1fr) minmax(0, 0.7fr) 64px 40px;
    align-items: center;
    column-gap: 16px;
    border-radius: var(--radius);
  }

  .head {
    margin-bottom: 8px;
    padding: 8px 8px 8px 16px;
    border-bottom: 1px solid var(--border);
    border-radius: 0;
    color: var(--text-muted);
    font-size: 0.875rem;
  }

  .head::after {
    content: '';
  }

  li.row {
    padding-right: 8px;
  }

  li.row:hover,
  li.row:focus-within {
    background: var(--surface);
  }

  .main {
    grid-column: 1 / 5;
    display: grid;
    grid-template-columns: subgrid;
    align-items: center;
    padding: 8px 0 8px 16px;
    border: 0;
    border-radius: var(--radius);
    background: none;
    text-align: left;
    cursor: pointer;
  }

  .num {
    position: relative;
    display: flex;
    justify-content: flex-end;
    align-items: center;
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
  }

  .hover-icon {
    position: absolute;
    right: 0;
    display: none;
    color: var(--text);
  }

  li.row:hover .num > :not(.hover-icon),
  .main:focus-visible .num > :not(.hover-icon) {
    visibility: hidden;
  }

  li.row:hover .hover-icon,
  .main:focus-visible .hover-icon {
    display: block;
  }

  .title {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .title-text,
  .title-sub,
  .artist {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .title-text {
    font-weight: 500;
  }

  .current .title-text {
    color: var(--accent);
  }

  .title-sub {
    display: none;
    font-size: 0.875rem;
    color: var(--text-muted);
  }

  .artist {
    color: var(--text-muted);
    font-size: 0.875rem;
  }

  .duration {
    text-align: right;
    color: var(--text-muted);
    font-size: 0.875rem;
    font-variant-numeric: tabular-nums;
  }

  .badge {
    display: inline-block;
    margin-left: 6px;
    padding: 1px 7px;
    border: 1px solid var(--accent);
    border-radius: 999px;
    color: var(--accent);
    font-size: 0.7rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    vertical-align: 1px;
  }

  /* Animated equalizer on the current track. */
  .eq {
    display: flex;
    align-items: flex-end;
    gap: 2px;
    height: 14px;
  }

  .eq i {
    width: 3px;
    height: 100%;
    border-radius: 1px;
    background: var(--accent);
    transform-origin: bottom;
    animation: eq 0.9s ease-in-out infinite alternate;
  }

  .eq i:nth-child(2) {
    animation-delay: -0.3s;
    animation-duration: 0.7s;
  }

  .eq i:nth-child(3) {
    animation-delay: -0.6s;
    animation-duration: 1.1s;
  }

  .eq.paused i {
    animation-play-state: paused;
  }

  @keyframes eq {
    from {
      transform: scaleY(0.25);
    }
    to {
      transform: scaleY(1);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .eq i {
      animation: none;
    }

    .eq i:nth-child(1) {
      transform: scaleY(0.6);
    }

    .eq i:nth-child(3) {
      transform: scaleY(0.4);
    }
  }

  /* "⋯" menu. */
  .menu-wrap {
    position: relative;
    grid-column: 5;
  }

  .more {
    display: grid;
    place-items: center;
    width: 36px;
    height: 36px;
    padding: 0;
    border: 0;
    border-radius: 50%;
    background: none;
    color: var(--text-muted);
    cursor: pointer;
    opacity: 0;
  }

  li.row:hover .more,
  li.row:focus-within .more,
  .more.open {
    opacity: 1;
  }

  .more:hover,
  .more.open {
    color: var(--text);
    background: var(--surface-hover);
  }

  @media (hover: none) {
    .more {
      opacity: 1;
    }
  }

  .menu {
    position: absolute;
    z-index: 20;
    right: 0;
    top: calc(100% + 4px);
    display: flex;
    flex-direction: column;
    min-width: 190px;
    padding: 4px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: 0 12px 32px rgb(0 0 0 / 0.55);
  }

  .menu.up {
    top: auto;
    bottom: calc(100% + 4px);
  }

  .menu button {
    padding: 10px 12px;
    border: 0;
    border-radius: 6px;
    background: none;
    text-align: left;
    font-size: 0.9rem;
    cursor: pointer;
  }

  .menu button:hover,
  .menu button:focus-visible {
    background: var(--surface-hover);
    outline: none;
  }

  @media (max-width: 760px) {
    .row {
      grid-template-columns: 28px minmax(0, 1fr) auto 36px;
      column-gap: 12px;
    }

    .head {
      padding: 8px;
    }

    .main {
      grid-column: 1 / 4;
      padding: 8px 0 8px 8px;
    }

    .menu-wrap {
      grid-column: 4;
    }

    li.row {
      padding-right: 4px;
    }

    .artist {
      display: none;
    }

    .title-sub {
      display: block;
    }
  }
</style>
