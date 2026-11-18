// Pure play-queue operations. They never mutate their inputs and know
// nothing about audio, so they are easy to reason about and to test.

export type Repeat = 'off' | 'all' | 'one';

/** A queue entry; `qid` tells apart two entries of the same track. */
export interface Entry<T> {
  qid: number;
  track: T;
}

/** The queue as played, plus the current position in it (-1 when empty). */
export interface QueueState<T> {
  items: Entry<T>[];
  index: number;
}

/**
 * Index to play after `index`, or null when playback should stop.
 * Repeat "one" only matters when a track ends by itself, which the caller
 * handles: an explicit "next" always moves on.
 */
export function nextIndex(length: number, index: number, repeat: Repeat): number | null {
  if (length === 0) return null;
  if (index + 1 < length) return index + 1;
  return repeat === 'all' ? 0 : null;
}

/** Index to play before `index`; null means "restart the current track". */
export function prevIndex(length: number, index: number, repeat: Repeat): number | null {
  if (length === 0) return null;
  if (index > 0) return index - 1;
  return repeat === 'all' && length > 1 ? length - 1 : null;
}

/** Fisher-Yates shuffle into a new array. */
export function shuffled<T>(items: readonly T[], random: () => number = Math.random): T[] {
  const out = items.slice();
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(random() * (i + 1));
    [out[i], out[j]] = [out[j], out[i]];
  }
  return out;
}

/**
 * Shuffles every entry but the current one, which moves to the front: what
 * comes next is random, and "previous" no longer leads back in album order.
 */
export function shuffleAround<T>(state: QueueState<T>, random: () => number = Math.random): QueueState<T> {
  const current = state.items[state.index];
  if (!current) return { items: shuffled(state.items, random), index: state.index };
  const others = state.items.filter((_, i) => i !== state.index);
  return { items: [current, ...shuffled(others, random)], index: 0 };
}

/**
 * Restores the original order when shuffle is turned off; playback carries
 * on from the current entry's place in that order.
 */
export function unshuffle<T>(original: Entry<T>[], current: Entry<T> | undefined): QueueState<T> {
  const index = current ? original.findIndex((e) => e.qid === current.qid) : -1;
  return { items: original.slice(), index: index >= 0 ? index : original.length > 0 ? 0 : -1 };
}

/** Inserts entries right after the current one. */
export function insertAfterCurrent<T>(state: QueueState<T>, entries: Entry<T>[]): QueueState<T> {
  const at = state.index + 1;
  const items = [...state.items.slice(0, at), ...entries, ...state.items.slice(at)];
  return { items, index: state.index < 0 && items.length > 0 ? 0 : state.index };
}

/** Appends entries at the end. */
export function append<T>(state: QueueState<T>, entries: Entry<T>[]): QueueState<T> {
  const items = [...state.items, ...entries];
  return { items, index: state.index < 0 && items.length > 0 ? 0 : state.index };
}

/**
 * Removes entry i. When the current entry is removed, the index points at
 * the entry that took its place (or the new last one).
 */
export function removeAt<T>(state: QueueState<T>, i: number): QueueState<T> & { removedCurrent: boolean } {
  if (i < 0 || i >= state.items.length) return { ...state, removedCurrent: false };
  const items = state.items.filter((_, k) => k !== i);
  let index = state.index;
  if (i < index) index -= 1;
  else if (i === index) index = Math.min(index, items.length - 1);
  return { items, index, removedCurrent: i === state.index };
}

/** Moves entry `from` to position `to`, keeping the same entry current. */
export function moveItem<T>(state: QueueState<T>, from: number, to: number): QueueState<T> {
  const n = state.items.length;
  if (from < 0 || from >= n || to < 0 || to >= n || from === to) return state;
  const items = state.items.slice();
  const [moved] = items.splice(from, 1);
  items.splice(to, 0, moved);
  let index = state.index;
  if (from === index) index = to;
  else if (from < index && to >= index) index -= 1;
  else if (from > index && to <= index) index += 1;
  return { items, index };
}

/** Removes the entries after the current one. */
export function clearUpcoming<T>(state: QueueState<T>): QueueState<T> {
  return { items: state.items.slice(0, state.index + 1), index: state.index };
}
