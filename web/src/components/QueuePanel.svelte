<script lang="ts">
  import { flip } from 'svelte/animate';
  import { player, trackCover, type QueueEntry } from '../lib/player.svelte';
  import { formatDuration } from '../lib/format';
  import Icon from './Icon.svelte';
  import placeholder from '../assets/cover-placeholder.svg';

  interface Props {
    onclose: () => void;
  }

  let { onclose }: Props = $props();

  let panel = $state<HTMLElement>();
  let list = $state<HTMLOListElement>();
  // Entry being dragged by its handle.
  let dragQid = $state<number | null>(null);

  $effect(() => {
    // Move the focus into the panel when it opens, back out when it closes.
    const previous = document.activeElement as HTMLElement | null;
    panel?.focus();
    return () => previous?.focus?.();
  });

  function onKeydown(e: KeyboardEvent): void {
    if (e.key === 'Escape') {
      e.stopPropagation();
      onclose();
    }
  }

  const first = $derived(player.index + 1);

  function startDrag(e: PointerEvent, entry: QueueEntry): void {
    if (e.button !== 0) return;
    e.preventDefault();
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    dragQid = entry.qid;
  }

  function dragMove(e: PointerEvent): void {
    if (dragQid === null || !list) return;
    const from = player.queue.findIndex((q) => q.qid === dragQid);
    if (from < 0) return;
    // Find the row under the pointer and move the dragged entry there.
    for (const row of list.querySelectorAll<HTMLElement>('[data-index]')) {
      const r = row.getBoundingClientRect();
      if (e.clientY >= r.top && e.clientY < r.bottom) {
        const to = Number(row.dataset.index);
        if (to !== from && to >= first) player.move(from, to);
        return;
      }
    }
  }

  function endDrag(): void {
    dragQid = null;
  }

  function cover(entry: QueueEntry): string {
    return trackCover(entry.track, 250) || placeholder;
  }
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<aside class="panel" aria-label="File d'attente" tabindex="-1" bind:this={panel} onkeydown={onKeydown}>
  <header class="head">
    <h2>File d'attente</h2>
    <button type="button" class="icon-btn" aria-label="Fermer la file d'attente" onclick={onclose}>
      <Icon name="close" />
    </button>
  </header>

  <div class="body">
    {#if player.current}
      <h3>En cours</h3>
      <div class="item current">
        <img class="art" src={cover(player.current)} alt="" width="40" height="40" />
        <div class="text">
          <span class="title">{player.current.track.title}</span>
          <span class="sub">{player.current.track.artist}</span>
        </div>
        <span class="dur">{formatDuration(player.current.track.durationMs)}</span>
      </div>
    {/if}

    <div class="next-head">
      <h3>À suivre</h3>
      {#if player.upcoming.length > 0}
        <button type="button" class="text-btn" onclick={() => player.clearUpcoming()}>Vider la file</button>
      {/if}
    </div>

    {#if player.upcoming.length === 0}
      <p class="empty">Rien à suivre. Ajoute des titres depuis ta bibliothèque.</p>
    {:else}
      <ol class="list" bind:this={list} aria-label="À suivre">
        {#each player.upcoming as entry, k (entry.qid)}
          {@const i = first + k}
          <li class="item" class:dragging={dragQid === entry.qid} data-index={i} animate:flip={{ duration: 150 }}>
            <span
              class="handle"
              aria-hidden="true"
              title="Glisser pour déplacer"
              onpointerdown={(e) => startDrag(e, entry)}
              onpointermove={dragMove}
              onpointerup={endDrag}
              onpointercancel={endDrag}
            >
              <Icon name="drag" size={18} />
            </span>
            <button type="button" class="pick" onclick={() => player.playAt(i)} aria-label="Lire {entry.track.title}">
              <img class="art" src={cover(entry)} alt="" width="40" height="40" loading="lazy" />
              <span class="text">
                <span class="title">{entry.track.title}</span>
                <span class="sub">{entry.track.artist}</span>
              </span>
            </button>
            <span class="actions">
              <button
                type="button"
                class="icon-btn small"
                aria-label="Monter {entry.track.title}"
                disabled={k === 0}
                onclick={() => player.move(i, i - 1)}
              >
                <Icon name="up" size={18} />
              </button>
              <button
                type="button"
                class="icon-btn small"
                aria-label="Descendre {entry.track.title}"
                disabled={k === player.upcoming.length - 1}
                onclick={() => player.move(i, i + 1)}
              >
                <Icon name="down" size={18} />
              </button>
              <button
                type="button"
                class="icon-btn small"
                aria-label="Retirer {entry.track.title} de la file"
                onclick={() => player.remove(i)}
              >
                <Icon name="close" size={18} />
              </button>
            </span>
          </li>
        {/each}
      </ol>
    {/if}
  </div>
</aside>

<style>
  .panel {
    position: fixed;
    z-index: 30;
    top: 8px;
    right: 8px;
    bottom: calc(var(--player-height) + 8px);
    display: flex;
    flex-direction: column;
    width: min(400px, calc(100vw - 16px));
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    background: var(--bg-elevated);
    box-shadow: 0 16px 48px rgb(0 0 0 / 0.6);
    outline: none;
    animation: slide-in 0.2s ease-out;
  }

  @keyframes slide-in {
    from {
      transform: translateX(24px);
      opacity: 0;
    }
  }

  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 16px 12px 8px 20px;
  }

  h2 {
    font-size: 1.1rem;
    font-weight: 800;
  }

  h3 {
    font-size: 0.95rem;
    font-weight: 700;
    color: var(--text-muted);
  }

  .body {
    flex: 1;
    overflow-y: auto;
    padding: 4px 12px 16px;
  }

  .body > h3 {
    padding: 8px 8px 6px;
  }

  .next-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 16px 8px 6px;
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px;
    border-radius: var(--radius);
  }

  .item:hover,
  .item.dragging {
    background: var(--surface);
  }

  .item.dragging {
    box-shadow: 0 6px 18px rgb(0 0 0 / 0.5);
  }

  .current {
    padding-left: 8px;
  }

  .current .title {
    color: var(--accent);
  }

  .handle {
    display: grid;
    place-items: center;
    width: 24px;
    height: 40px;
    color: var(--text-faint);
    cursor: grab;
    touch-action: none;
  }

  .dragging .handle {
    cursor: grabbing;
  }

  .pick {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 0;
    border: 0;
    background: none;
    text-align: left;
    cursor: pointer;
  }

  .art {
    flex: none;
    width: 40px;
    height: 40px;
    border-radius: 4px;
    object-fit: cover;
    background: var(--surface);
  }

  .text {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }

  .title,
  .sub {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .title {
    font-size: 0.9rem;
    font-weight: 600;
  }

  .sub {
    font-size: 0.8rem;
    color: var(--text-muted);
  }

  .dur {
    font-size: 0.8rem;
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
  }

  .actions {
    display: flex;
    opacity: 0;
    transition: opacity 0.15s ease;
  }

  .item:hover .actions,
  .item:focus-within .actions {
    opacity: 1;
  }

  @media (hover: none) {
    .actions {
      opacity: 1;
    }
  }

  .icon-btn {
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
  }

  .icon-btn.small {
    width: 30px;
    height: 30px;
  }

  .icon-btn:hover:not(:disabled) {
    color: var(--text);
    background: var(--surface-hover);
  }

  .icon-btn:disabled {
    opacity: 0.3;
    cursor: default;
  }

  .text-btn {
    padding: 4px 10px;
    border: 0;
    border-radius: 999px;
    background: none;
    color: var(--text-muted);
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
  }

  .text-btn:hover {
    color: var(--text);
    background: var(--surface-hover);
  }

  .empty {
    padding: 4px 8px;
    color: var(--text-muted);
    font-size: 0.9rem;
  }

  @media (max-width: 760px) {
    .panel {
      inset: 0;
      width: auto;
      border: 0;
      border-radius: 0;
      animation-name: slide-up;
    }

    @keyframes slide-up {
      from {
        transform: translateY(24px);
        opacity: 0;
      }
    }
  }
</style>
