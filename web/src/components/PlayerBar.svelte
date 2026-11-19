<script lang="ts">
  import { player, qualities, trackCover, type Quality } from '../lib/player.svelte';
  import Icon from './Icon.svelte';
  import QueuePanel from './QueuePanel.svelte';
  import SeekBar from './SeekBar.svelte';
  import placeholder from '../assets/cover-placeholder.svg';

  let queueOpen = $state(false);
  let sheet = $state<HTMLDialogElement>();
  let sheetOpen = $state(false);

  const track = $derived(player.current?.track);
  const cover = $derived(trackCover(track, 250) || placeholder);
  const bigCover = $derived(trackCover(track, 500) || placeholder);
  const albumHref = $derived(track?.albumKey ? `/library/album/${encodeURIComponent(track.albumKey)}` : '');
  const progress = $derived(player.duration > 0 ? Math.min(100, (player.position / player.duration) * 100) : 0);
  const repeatLabel = $derived(
    player.repeat === 'off'
      ? 'Répéter : désactivé'
      : player.repeat === 'all'
        ? 'Répéter : toute la file'
        : 'Répéter : le titre',
  );
  const volumeIcon = $derived(player.muted || player.volume === 0 ? 'mute' : player.volume < 0.5 ? 'volumeLow' : 'volume');

  function openSheet(): void {
    sheet?.showModal();
    sheetOpen = true;
  }

  function closeSheet(): void {
    sheet?.close();
  }

  function openQueueFromSheet(): void {
    closeSheet();
    queueOpen = true;
  }

  // Close the sheet when following a link inside it (title, album).
  function onSheetClick(e: MouseEvent): void {
    if (e.target instanceof Element && e.target.closest('a')) closeSheet();
  }

  function isTyping(el: EventTarget | null): boolean {
    if (!(el instanceof HTMLElement)) return false;
    return el.isContentEditable || el.closest('input, textarea, select, [contenteditable=""], [contenteditable="true"]') !== null;
  }

  function onKeydown(e: KeyboardEvent): void {
    if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey || !player.current) return;
    if (isTyping(e.target)) return;
    if (e.key === ' ' || e.code === 'Space') {
      // Space on a focused control activates that control instead.
      if (e.target instanceof Element && e.target.closest('button, a, summary, [role="button"], [role="menuitem"]')) return;
      e.preventDefault();
      player.toggle();
    } else if (e.key === 'ArrowRight') {
      e.preventDefault();
      if (e.shiftKey) player.next();
      else player.seekBy(10);
    } else if (e.key === 'ArrowLeft') {
      e.preventDefault();
      if (e.shiftKey) player.prev();
      else player.seekBy(-10);
    }
  }
</script>

<svelte:window onkeydown={onKeydown} />

{#snippet playButton(big: boolean)}
  <button
    type="button"
    class="play"
    class:big
    class:busy={player.buffering}
    aria-label={player.playing ? 'Pause' : 'Lecture'}
    aria-busy={player.buffering}
    disabled={!track}
    onclick={() => player.toggle()}
  >
    <Icon name={player.playing ? 'pause' : 'play'} size={big ? 30 : 20} />
    {#if player.buffering}<span class="spinner" aria-hidden="true"></span>{/if}
  </button>
{/snippet}

{#snippet controls(big: boolean)}
  <div class="buttons" class:big>
    <button
      type="button"
      class="ctrl toggle"
      class:on={player.shuffle}
      aria-label="Aléatoire"
      aria-pressed={player.shuffle}
      title="Aléatoire"
      disabled={!track}
      onclick={() => player.setShuffle(!player.shuffle)}
    >
      <Icon name="shuffle" size={big ? 24 : 18} />
    </button>
    <button type="button" class="ctrl" aria-label="Précédent" title="Précédent" disabled={!track} onclick={() => player.prev()}>
      <Icon name="prev" size={big ? 32 : 20} />
    </button>
    {@render playButton(big)}
    <button type="button" class="ctrl" aria-label="Suivant" title="Suivant" disabled={!track} onclick={() => player.next()}>
      <Icon name="next" size={big ? 32 : 20} />
    </button>
    <button
      type="button"
      class="ctrl toggle"
      class:on={player.repeat !== 'off'}
      aria-label={repeatLabel}
      aria-pressed={player.repeat !== 'off'}
      title={repeatLabel}
      disabled={!track}
      onclick={() => player.cycleRepeat()}
    >
      <Icon name="repeat" size={big ? 24 : 18} />
      {#if player.repeat === 'one'}<span class="one" aria-hidden="true">1</span>{/if}
    </button>
  </div>
{/snippet}

{#snippet qualitySelect()}
  <label class="quality">
    <span class="visually-hidden">Qualité de lecture</span>
    <select
      value={player.quality}
      title="Qualité de lecture"
      onchange={(e) => player.setQuality(e.currentTarget.value as Quality)}
    >
      {#each qualities as q (q.value)}
        <option value={q.value}>{q.label}</option>
      {/each}
    </select>
  </label>
{/snippet}

<footer class="player" aria-label="Lecteur">
  <!-- Desktop: three columns. Mobile: compact bar that opens the sheet. -->
  <div class="now">
    {#if track}
      <img class="art" src={cover} alt="" width="56" height="56" />
      <div class="now-text">
        {#if albumHref}
          <a class="now-title" href={albumHref} title={track.title}>{track.title}</a>
        {:else}
          <p class="now-title" title={track.title}>{track.title}</p>
        {/if}
        <p class="now-sub">{track.artist}</p>
      </div>
    {:else}
      <div class="art empty" aria-hidden="true"><Icon name="note" size={22} /></div>
      <div class="now-text">
        <p class="now-title idle">Aucune lecture en cours</p>
        <p class="now-sub">Choisis un album pour commencer</p>
      </div>
    {/if}
    <button type="button" class="expand" aria-label="Ouvrir le lecteur" disabled={!track} onclick={openSheet}></button>
  </div>

  <div class="center">
    {@render controls(false)}
    <div class="seek-wrap"><SeekBar /></div>
  </div>

  <div class="side">
    {@render qualitySelect()}
    <button
      type="button"
      class="ctrl toggle"
      class:on={queueOpen}
      aria-label="File d'attente"
      aria-expanded={queueOpen}
      title="File d'attente"
      onclick={() => (queueOpen = !queueOpen)}
    >
      <Icon name="queue" size={20} />
    </button>
    <div class="volume">
      <button
        type="button"
        class="ctrl"
        aria-label={player.muted ? 'Réactiver le son' : 'Couper le son'}
        title={player.muted ? 'Réactiver le son' : 'Couper le son'}
        onclick={() => player.toggleMute()}
      >
        <Icon name={volumeIcon} size={20} />
      </button>
      <input
        type="range"
        min="0"
        max="1"
        step="0.01"
        value={player.muted ? 0 : player.volume}
        aria-label="Volume"
        aria-valuetext="{Math.round((player.muted ? 0 : player.volume) * 100)} %"
        style:--pct="{(player.muted ? 0 : player.volume) * 100}%"
        oninput={(e) => player.setVolume(Number(e.currentTarget.value))}
      />
    </div>
  </div>

  <div class="mobile-play">{@render playButton(false)}</div>
  <div class="line" aria-hidden="true"><span style:width="{progress}%"></span></div>
</footer>

{#if player.toast}
  {#key player.toast.id}
    <div class="toast" role="status">
      <span>{player.toast.text}</span>
      <button type="button" aria-label="Fermer le message" onclick={() => player.dismissToast()}>
        <Icon name="close" size={16} />
      </button>
    </div>
  {/key}
{/if}

{#if queueOpen}
  <QueuePanel onclose={() => (queueOpen = false)} />
{/if}

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
<dialog
  class="sheet"
  aria-label="Lecteur"
  bind:this={sheet}
  onclose={() => (sheetOpen = false)}
  onclick={onSheetClick}
>
  {#if sheetOpen && track}
    <div class="sheet-inner">
      <header class="sheet-head">
        <button type="button" class="ctrl" aria-label="Fermer le lecteur" onclick={closeSheet}>
          <Icon name="chevronDown" size={28} />
        </button>
        <p>En cours de lecture</p>
        <span class="ctrl" aria-hidden="true"></span>
      </header>
      <img class="sheet-art" src={bigCover} alt="Pochette de {track.album}" width="500" height="500" />
      <div class="sheet-text">
        {#if albumHref}
          <a class="sheet-title" href={albumHref}>{track.title}</a>
        {:else}
          <p class="sheet-title">{track.title}</p>
        {/if}
        <p class="sheet-sub">{track.artist}{track.album ? ` · ${track.album}` : ''}</p>
      </div>
      <SeekBar large />
      {@render controls(true)}
      <div class="sheet-foot">
        {@render qualitySelect()}
        <button type="button" class="ctrl" aria-label="File d'attente" title="File d'attente" onclick={openQueueFromSheet}>
          <Icon name="queue" size={24} />
        </button>
      </div>
    </div>
  {/if}
</dialog>

<style>
  .player {
    grid-area: player;
    position: relative;
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 2fr) minmax(0, 1fr);
    align-items: center;
    gap: 16px;
    padding: 0 16px;
    background: var(--bg-sidebar);
  }

  /* ---- Now playing ---- */

  .now {
    position: relative;
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }

  .art {
    flex: none;
    width: 56px;
    height: 56px;
    border-radius: 6px;
    object-fit: cover;
    background: var(--surface);
  }

  .art.empty {
    display: grid;
    place-items: center;
    color: var(--text-faint);
  }

  .now-text {
    min-width: 0;
    display: flex;
    flex-direction: column;
  }

  .now-title,
  .now-sub {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .now-title {
    font-size: 0.9rem;
    font-weight: 600;
  }

  .now-title.idle {
    color: var(--text-muted);
  }

  .now-sub {
    font-size: 0.78rem;
    color: var(--text-muted);
  }

  /* Only used on mobile, where it covers the cover and the text. */
  .expand {
    display: none;
  }

  /* ---- Controls ---- */

  .center {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    min-width: 0;
  }

  .seek-wrap {
    width: 100%;
    max-width: 620px;
  }

  .buttons {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .buttons.big {
    justify-content: space-between;
    width: 100%;
  }

  .ctrl {
    position: relative;
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    padding: 0;
    border: 0;
    border-radius: 50%;
    background: none;
    color: var(--text-muted);
    cursor: pointer;
    transition: color 0.15s ease, transform 0.1s ease;
  }

  .ctrl:hover:not(:disabled) {
    color: var(--text);
  }

  .ctrl:active:not(:disabled) {
    transform: scale(0.94);
  }

  .ctrl:disabled {
    color: var(--text-faint);
    cursor: default;
  }

  .toggle.on {
    color: var(--accent);
  }

  .toggle.on::after {
    content: '';
    position: absolute;
    bottom: 1px;
    left: 50%;
    width: 4px;
    height: 4px;
    margin-left: -2px;
    border-radius: 50%;
    background: currentColor;
  }

  .one {
    position: absolute;
    top: 3px;
    right: 3px;
    min-width: 13px;
    height: 13px;
    border-radius: 50%;
    background: var(--accent);
    color: var(--on-accent);
    font-size: 0.6rem;
    font-weight: 800;
    line-height: 13px;
    text-align: center;
  }

  .big .ctrl {
    width: 52px;
    height: 52px;
  }

  .play {
    position: relative;
    display: grid;
    place-items: center;
    width: 36px;
    height: 36px;
    padding: 0;
    border: 0;
    border-radius: 50%;
    background: var(--text);
    color: var(--bg);
    cursor: pointer;
    transition: transform 0.1s ease, background-color 0.15s ease;
  }

  .play:hover:not(:disabled) {
    transform: scale(1.06);
  }

  .play:active:not(:disabled) {
    transform: scale(0.96);
  }

  .play:disabled {
    background: var(--surface-hover);
    color: var(--text-faint);
    cursor: default;
  }

  .play.big {
    width: 68px;
    height: 68px;
  }

  .spinner {
    position: absolute;
    inset: -4px;
    border: 2px solid transparent;
    border-top-color: var(--accent);
    border-right-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
    pointer-events: none;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    /* A static ring instead of a spinning one. */
    .spinner {
      animation: none;
      border-color: var(--accent);
      opacity: 0.6;
    }
  }

  /* ---- Right side ---- */

  .side {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
    min-width: 0;
  }

  .quality select {
    max-width: 112px;
    padding: 5px 8px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    color: var(--text-muted);
    font-size: 0.8rem;
    cursor: pointer;
  }

  .quality select:hover {
    color: var(--text);
  }

  .volume {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .volume input {
    width: 96px;
    height: 16px;
    margin: 0;
    background: transparent;
    cursor: pointer;
    appearance: none;
    -webkit-appearance: none;
  }

  .volume input::-webkit-slider-runnable-track {
    height: 4px;
    border-radius: 999px;
    background: linear-gradient(to right, var(--text) var(--pct), var(--surface-hover) var(--pct));
  }

  .volume input::-moz-range-track {
    height: 4px;
    border-radius: 999px;
    background: linear-gradient(to right, var(--text) var(--pct), var(--surface-hover) var(--pct));
  }

  .volume input::-webkit-slider-thumb {
    -webkit-appearance: none;
    width: 12px;
    height: 12px;
    margin-top: -4px;
    border: 0;
    border-radius: 50%;
    background: var(--text);
  }

  .volume input::-moz-range-thumb {
    width: 12px;
    height: 12px;
    border: 0;
    border-radius: 50%;
    background: var(--text);
  }

  .volume:hover input::-webkit-slider-runnable-track {
    background: linear-gradient(to right, var(--accent) var(--pct), var(--surface-hover) var(--pct));
  }

  .volume:hover input::-moz-range-track {
    background: linear-gradient(to right, var(--accent) var(--pct), var(--surface-hover) var(--pct));
  }

  .mobile-play,
  .line {
    display: none;
  }

  @media (max-width: 1000px) {
    .volume input {
      width: 64px;
    }
  }

  /* ---- Toast ---- */

  .toast {
    position: fixed;
    z-index: 40;
    left: 50%;
    bottom: calc(var(--player-height) + 16px);
    display: flex;
    align-items: center;
    gap: 8px;
    max-width: min(520px, calc(100vw - 32px));
    padding: 10px 10px 10px 16px;
    border-radius: var(--radius);
    background: var(--text);
    color: var(--bg);
    font-size: 0.9rem;
    font-weight: 600;
    box-shadow: 0 8px 24px rgb(0 0 0 / 0.5);
    transform: translateX(-50%);
    animation: toast-in 0.2s ease-out;
  }

  .toast button {
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    padding: 0;
    border: 0;
    border-radius: 50%;
    background: none;
    color: inherit;
    cursor: pointer;
  }

  @keyframes toast-in {
    from {
      opacity: 0;
      transform: translate(-50%, 8px);
    }
  }

  /* ---- Full-screen sheet (mobile) ---- */

  .sheet {
    width: 100%;
    max-width: none;
    height: 100%;
    max-height: none;
    margin: 0;
    padding: 0;
    border: 0;
    background: linear-gradient(180deg, color-mix(in srgb, var(--accent) 18%, var(--bg)) 0, var(--bg) 60%);
    color: var(--text);
  }

  .sheet[open] {
    animation: sheet-in 0.25s ease-out;
  }

  .sheet::backdrop {
    background: rgb(0 0 0 / 0.6);
  }

  @keyframes sheet-in {
    from {
      transform: translateY(32px);
      opacity: 0;
    }
  }

  .sheet-inner {
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: 20px;
    max-width: 480px;
    height: 100%;
    margin: 0 auto;
    padding: max(12px, env(safe-area-inset-top)) 24px max(24px, env(safe-area-inset-bottom));
  }

  .sheet-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    font-size: 0.8rem;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-muted);
  }

  .sheet-head .ctrl {
    width: 44px;
    height: 44px;
    color: var(--text);
  }

  .sheet-art {
    width: 100%;
    max-height: 46vh;
    aspect-ratio: 1;
    object-fit: cover;
    align-self: center;
    border-radius: var(--radius-lg);
    background: var(--surface);
    box-shadow: 0 16px 40px rgb(0 0 0 / 0.5);
  }

  .sheet-text {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
    margin-top: auto;
  }

  .sheet-title {
    font-size: 1.4rem;
    font-weight: 800;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .sheet-sub {
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .sheet-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .sheet-foot .ctrl {
    width: 44px;
    height: 44px;
  }

  /* ---- Mobile compact bar ---- */

  @media (max-width: 760px) {
    .player {
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 8px;
      height: 60px;
      margin: 0 8px 6px;
      padding: 0 8px 0 6px;
      border-radius: var(--radius);
      background: var(--surface);
      overflow: hidden;
    }

    .art {
      width: 44px;
      height: 44px;
    }

    .expand:not(:disabled) {
      display: block;
      position: absolute;
      inset: 0;
      width: 100%;
      padding: 0;
      border: 0;
      background: none;
      cursor: pointer;
    }

    .center,
    .side {
      display: none;
    }

    .mobile-play {
      display: block;
    }

    .line {
      display: block;
      position: absolute;
      left: 8px;
      right: 8px;
      bottom: 0;
      height: 2px;
      border-radius: 2px;
      background: var(--surface-hover);
      overflow: hidden;
    }

    .line span {
      display: block;
      height: 100%;
      background: var(--text);
    }

    .toast {
      bottom: calc(var(--bottom-nav-height) + 76px);
    }
  }
</style>
