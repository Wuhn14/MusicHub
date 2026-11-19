<script lang="ts">
  import { player } from '../lib/player.svelte';
  import { formatClock } from '../lib/format';

  interface Props {
    /** Larger thumb and times below the bar (full-screen player). */
    large?: boolean;
  }

  let { large = false }: Props = $props();

  // While the thumb is dragged, the bar shows the drag position and the
  // seek happens only on release.
  let dragging = $state(false);
  let dragValue = $state(0);

  // Keyboard seeks on a converted stream are grouped: each one restarts the
  // conversion, so only the last key press of a burst seeks.
  const keySeekDelayMs = 350;
  let fromKeyboard = false;
  let keyTimer: ReturnType<typeof setTimeout> | undefined;

  function seekNow(seconds: number): void {
    clearTimeout(keyTimer);
    keyTimer = undefined;
    dragging = false;
    player.seek(seconds);
  }

  function changed(seconds: number): void {
    if (fromKeyboard && player.converted) {
      dragging = true;
      dragValue = seconds;
      clearTimeout(keyTimer);
      keyTimer = setTimeout(() => seekNow(seconds), keySeekDelayMs);
      return;
    }
    seekNow(seconds);
  }

  /** Ends a drag that no change event will close (released outside, focus lost). */
  function release(): void {
    if (keyTimer !== undefined) {
      seekNow(dragValue);
      return;
    }
    dragging = false;
  }

  $effect(() => () => clearTimeout(keyTimer));

  const max = $derived(player.duration);
  const value = $derived(Math.min(dragging ? dragValue : player.position, max || 0));
  const pct = $derived(max > 0 ? (value / max) * 100 : 0);
</script>

<div class="seek" class:large>
  <span class="time elapsed">{formatClock(value)}</span>
  <input
    type="range"
    min="0"
    max={max > 0 ? max : 1}
    step="1"
    {value}
    disabled={!player.current || !(max > 0)}
    aria-label="Position dans le titre"
    aria-valuetext="{formatClock(value)} sur {formatClock(max)}"
    style:--pct="{pct}%"
    onkeydown={() => (fromKeyboard = true)}
    onpointerdown={() => (fromKeyboard = false)}
    onpointerup={() => {
      if (keyTimer === undefined) dragging = false;
    }}
    onpointercancel={release}
    onblur={release}
    oninput={(e) => {
      dragging = true;
      dragValue = Number(e.currentTarget.value);
    }}
    onchange={(e) => changed(Number(e.currentTarget.value))}
  />
  <span class="time total">{formatClock(max)}</span>
</div>

<style>
  .seek {
    display: grid;
    grid-template-columns: 44px minmax(0, 1fr) 44px;
    align-items: center;
    gap: 8px;
    width: 100%;
  }

  .time {
    font-size: 0.75rem;
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
  }

  .elapsed {
    text-align: right;
  }

  .large {
    grid-template-columns: 1fr 1fr;
    gap: 6px 0;
  }

  .large input {
    grid-column: 1 / -1;
    grid-row: 1;
  }

  .large .elapsed {
    text-align: left;
  }

  .large .total {
    text-align: right;
  }

  input {
    --track: 4px;
    width: 100%;
    height: 16px;
    margin: 0;
    background: transparent;
    cursor: pointer;
    appearance: none;
    -webkit-appearance: none;
  }

  input:disabled {
    cursor: default;
  }

  input::-webkit-slider-runnable-track {
    height: var(--track);
    border-radius: 999px;
    background: linear-gradient(to right, var(--text) var(--pct), var(--surface-hover) var(--pct));
  }

  input::-moz-range-track {
    height: var(--track);
    border-radius: 999px;
    background: linear-gradient(to right, var(--text) var(--pct), var(--surface-hover) var(--pct));
  }

  input::-webkit-slider-thumb {
    -webkit-appearance: none;
    width: 12px;
    height: 12px;
    margin-top: calc((var(--track) - 12px) / 2);
    border: 0;
    border-radius: 50%;
    background: var(--text);
    opacity: 0;
    transition: opacity 0.15s ease;
  }

  input::-moz-range-thumb {
    width: 12px;
    height: 12px;
    border: 0;
    border-radius: 50%;
    background: var(--text);
    opacity: 0;
    transition: opacity 0.15s ease;
  }

  .seek:hover input:not(:disabled)::-webkit-slider-runnable-track,
  input:focus-visible::-webkit-slider-runnable-track {
    background: linear-gradient(to right, var(--accent) var(--pct), var(--surface-hover) var(--pct));
  }

  .seek:hover input:not(:disabled)::-moz-range-track,
  input:focus-visible::-moz-range-track {
    background: linear-gradient(to right, var(--accent) var(--pct), var(--surface-hover) var(--pct));
  }

  .seek:hover input:not(:disabled)::-webkit-slider-thumb,
  input:focus-visible::-webkit-slider-thumb,
  .large input:not(:disabled)::-webkit-slider-thumb {
    opacity: 1;
  }

  .seek:hover input:not(:disabled)::-moz-range-thumb,
  input:focus-visible::-moz-range-thumb,
  .large input:not(:disabled)::-moz-range-thumb {
    opacity: 1;
  }

  .large input {
    --track: 5px;
    height: 24px;
  }

  @media (hover: none) {
    input:not(:disabled)::-webkit-slider-thumb {
      opacity: 1;
    }

    input:not(:disabled)::-moz-range-thumb {
      opacity: 1;
    }
  }
</style>
