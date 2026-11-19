// The web player: one module-level state object driving a single
// HTMLAudioElement. Components read `player` and call its methods; nothing
// else touches the audio element.

import type { LibTrack } from './api';
import {
  append,
  clearUpcoming,
  insertAfterCurrent,
  moveItem,
  nextIndex,
  prevIndex,
  removeAt,
  shuffleAround,
  unshuffle,
  type Entry,
  type QueueState,
  type Repeat,
} from './queue';

export type { Repeat } from './queue';
export type Quality = 'original' | '320' | '192' | '128';

export const qualities: { value: Quality; label: string }[] = [
  { value: 'original', label: 'Originale' },
  { value: '320', label: 'MP3 320' },
  { value: '192', label: 'MP3 192' },
  { value: '128', label: 'MP3 128' },
];

export interface PlayableTrack {
  fileId: string;
  title: string;
  artist: string;
  album: string;
  /** Cover path without size ("/api/covers/<id>"), or '' when unknown. */
  coverUrl: string;
  durationMs: number;
  /** Library album key, used to link back to the album page. */
  albumKey?: string;
}

export type QueueEntry = Entry<PlayableTrack>;

/** Keeps only the fields the player needs (and stores). */
export function toPlayable(t: LibTrack | PlayableTrack): PlayableTrack {
  return {
    fileId: t.fileId,
    title: t.title,
    artist: t.artist,
    album: t.album,
    coverUrl: t.coverUrl,
    durationMs: t.durationMs,
    albumKey: t.albumKey,
  };
}

/** Sized cover URL for a track, or '' when it has none. */
export function trackCover(t: PlayableTrack | undefined, size: 250 | 500): string {
  return t?.coverUrl ? `${t.coverUrl}?size=${size}` : '';
}

const storageKey = 'musichub.player.v1';
const prevRestartSeconds = 3;
const seekStepSeconds = 10;
const skipAfterErrorMs = 3000;
const busyMessage = 'Trop de conversions en cours, réessaie dans un instant.';

function isQuality(v: unknown): v is Quality {
  return v === 'original' || v === '320' || v === '192' || v === '128';
}

function isRepeat(v: unknown): v is Repeat {
  return v === 'off' || v === 'all' || v === 'one';
}

function defaultQuality(): Quality {
  const nav = navigator as Navigator & { connection?: { type?: string } };
  return nav.connection?.type === 'cellular' ? '192' : 'original';
}

function isTrack(v: unknown): v is PlayableTrack {
  if (!v || typeof v !== 'object') return false;
  const t = v as Record<string, unknown>;
  return (
    typeof t.fileId === 'string' &&
    t.fileId !== '' &&
    typeof t.title === 'string' &&
    typeof t.artist === 'string' &&
    typeof t.album === 'string' &&
    typeof t.coverUrl === 'string' &&
    typeof t.durationMs === 'number'
  );
}

interface Saved {
  queue: PlayableTrack[];
  /** Positions in `queue` of the unshuffled order, when shuffle is on. */
  original: number[] | null;
  index: number;
  position: number;
  volume: number;
  muted: boolean;
  quality: Quality;
  shuffle: boolean;
  repeat: Repeat;
}

class Player {
  /** The queue in play order. */
  queue = $state<QueueEntry[]>([]);
  /** Index of the current entry in `queue`, -1 when the queue is empty. */
  index = $state(-1);
  /** True while playback is wanted (playing, or about to). */
  playing = $state(false);
  /** True while the audio waits for data. */
  buffering = $state(false);
  /** Displayed position in seconds, from the start of the track. */
  position = $state(0);
  volume = $state(1);
  muted = $state(false);
  quality = $state<Quality>('original');
  shuffle = $state(false);
  repeat = $state<Repeat>('off');
  /** A short message for the user (errors, confirmations). */
  toast = $state<{ id: number; text: string } | null>(null);

  readonly current = $derived<QueueEntry | undefined>(this.queue[this.index]);
  /** Displayed duration in seconds, from the library (not the stream). */
  readonly duration = $derived((this.current?.track.durationMs ?? 0) / 1000);
  readonly upcoming = $derived(this.queue.slice(this.index + 1));

  // The unshuffled order while shuffle is on (not reactive: never displayed).
  private original: QueueEntry[] | null = null;
  private audio: HTMLAudioElement | null = null;
  // Seconds of the track skipped by a transcoded stream started with `start`.
  private offset = 0;
  // Whether the audio element holds the current entry's stream.
  private loaded = false;
  private loadedQuality: Quality = 'original';
  // Seek to apply once the metadata of an original stream is known.
  private pendingSeek: number | null = null;
  private nextQid = 1;
  private toastId = 0;
  private toastTimer: ReturnType<typeof setTimeout> | undefined;
  private skipTimer: ReturnType<typeof setTimeout> | undefined;
  private saveTimer: ReturnType<typeof setTimeout> | undefined;
  private lastSave = 0;
  private lastPositionState = 0;
  // Set by pause(), cleared by play() and load(): a "playing" event that
  // arrives after the user paused must not flip the state back.
  private userPaused = false;

  constructor() {
    if (typeof window === 'undefined') return;
    this.restore();
    window.addEventListener('pagehide', () => this.saveNow());
    // Mobile browsers may kill a hidden page without any pagehide.
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'hidden') this.saveNow();
    });
  }

  /** True when the loaded stream is converted: seeking restarts it. */
  get converted(): boolean {
    return this.loaded && this.loadedQuality !== 'original';
  }

  // ---- Public API -------------------------------------------------------

  /** Replaces the queue with `tracks` and plays tracks[startIndex]. */
  playList(tracks: PlayableTrack[], startIndex = 0): void {
    if (tracks.length === 0) return;
    const start = Math.min(Math.max(0, startIndex), tracks.length - 1);
    let state: QueueState<PlayableTrack> = { items: this.entries(tracks), index: start };
    this.original = null;
    if (this.shuffle) {
      this.original = state.items.slice();
      state = shuffleAround(state);
    }
    this.setState(state);
    this.load(0, true);
  }

  /** Inserts a track right after the current one. */
  playNext(t: PlayableTrack): void {
    const entries = this.entries([t]);
    const wasEmpty = this.queue.length === 0;
    if (this.original) {
      const cur = this.current;
      const at = cur ? this.original.findIndex((e) => e.qid === cur.qid) + 1 : this.original.length;
      this.original.splice(at, 0, ...entries);
    }
    this.setState(insertAfterCurrent(this.state(), entries));
    if (wasEmpty) this.load(0, true);
    else this.notify(`« ${t.title} » sera lu ensuite.`);
  }

  /** Appends a track at the end of the queue. */
  addToQueue(t: PlayableTrack): void {
    const entries = this.entries([t]);
    const wasEmpty = this.queue.length === 0;
    this.original?.push(...entries);
    this.setState(append(this.state(), entries));
    if (wasEmpty) this.load(0, true);
    else this.notify(`« ${t.title} » ajouté à la file.`);
  }

  /** Plays the queue entry at index i. */
  playAt(i: number): void {
    if (i < 0 || i >= this.queue.length) return;
    this.index = i;
    this.load(0, true);
  }

  toggle(): void {
    if (this.playing) this.pause();
    else this.play();
  }

  play(): void {
    if (!this.current) return;
    this.cancelSkip();
    if (!this.loaded) {
      this.load(this.position, true);
      return;
    }
    this.userPaused = false;
    this.playing = true;
    this.startAudio();
    this.updatePositionState();
  }

  pause(): void {
    this.cancelSkip();
    this.userPaused = true;
    this.playing = false;
    this.buffering = false;
    this.audio?.pause();
    this.updatePositionState();
    this.saveSoon();
  }

  next(): void {
    const i = nextIndex(this.queue.length, this.index, this.repeat === 'one' ? 'all' : this.repeat);
    if (i === null) return;
    this.goTo(i);
  }

  prev(): void {
    if (!this.current) return;
    const i = prevIndex(this.queue.length, this.index, this.repeat);
    if (this.position > prevRestartSeconds || i === null) {
      this.seek(0);
      return;
    }
    this.goTo(i);
  }

  /** Moves to `seconds` from the start of the current track. */
  seek(seconds: number): void {
    if (!this.current) return;
    const max = this.duration > 1 ? this.duration - 0.5 : Infinity;
    const s = Math.min(Math.max(0, seconds), max);
    this.cancelSkip();
    if (!this.loaded) {
      // Restored but never started: remember where to start.
      this.position = s;
      this.saveSoon();
      return;
    }
    if (this.loadedQuality === 'original') {
      const audio = this.ensureAudio();
      if (audio.readyState >= HTMLMediaElement.HAVE_METADATA) audio.currentTime = s;
      else this.pendingSeek = s;
      this.position = s;
      this.updatePositionState();
    } else {
      // A transcoded stream cannot seek: restart it at the new position.
      this.load(s, this.playing);
    }
  }

  seekBy(delta: number): void {
    this.seek(this.position + delta);
  }

  setVolume(v: number): void {
    this.volume = Math.min(1, Math.max(0, v));
    if (this.volume > 0) this.muted = false;
    this.applyVolume();
    this.saveSoon();
  }

  toggleMute(): void {
    this.muted = !this.muted;
    if (!this.muted && this.volume === 0) this.volume = 0.5;
    this.applyVolume();
    this.saveSoon();
  }

  setQuality(q: Quality): void {
    if (q === this.quality) return;
    this.quality = q;
    this.saveSoon();
    // Restart the current track where it is, in the new quality.
    if (this.loaded) this.load(this.position, this.playing);
  }

  setShuffle(on: boolean): void {
    if (on === this.shuffle) return;
    this.shuffle = on;
    if (on) {
      this.original = this.queue.slice();
      this.setState(shuffleAround(this.state()));
    } else if (this.original) {
      this.setState(unshuffle(this.original, this.current));
      this.original = null;
    }
    this.saveSoon();
  }

  setRepeat(r: Repeat): void {
    this.repeat = r;
    this.saveSoon();
  }

  /** Cycles off → all → one → off. */
  cycleRepeat(): void {
    this.setRepeat(this.repeat === 'off' ? 'all' : this.repeat === 'all' ? 'one' : 'off');
  }

  /** Removes queue entry i; removing the current entry plays the next one. */
  remove(i: number): void {
    const entry = this.queue[i];
    if (!entry) return;
    if (this.original) this.original = this.original.filter((e) => e.qid !== entry.qid);
    const { removedCurrent, ...state } = removeAt(this.state(), i);
    this.setState(state);
    if (!removedCurrent) return;
    if (this.queue.length === 0) {
      this.stop();
    } else {
      this.goTo(this.index);
    }
  }

  move(from: number, to: number): void {
    this.setState(moveItem(this.state(), from, to));
  }

  /** Empties « À suivre »; the current track keeps playing. */
  clearUpcoming(): void {
    const kept = new Set(this.queue.slice(0, this.index + 1).map((e) => e.qid));
    if (this.original) this.original = this.original.filter((e) => kept.has(e.qid));
    this.setState(clearUpcoming(this.state()));
  }

  dismissToast(): void {
    clearTimeout(this.toastTimer);
    this.toast = null;
  }

  // ---- Queue state helpers ----------------------------------------------

  /** Makes entry i current, keeping the play/pause state. */
  private goTo(i: number): void {
    this.index = i;
    if (this.loaded || this.playing) {
      this.load(0, this.playing);
    } else {
      this.position = 0;
      this.saveSoon();
    }
  }

  private entries(tracks: PlayableTrack[]): QueueEntry[] {
    return tracks.map((t) => ({ qid: this.nextQid++, track: toPlayable(t) }));
  }

  private state(): QueueState<PlayableTrack> {
    return { items: this.queue, index: this.index };
  }

  private setState(s: QueueState<PlayableTrack>): void {
    this.queue = s.items;
    this.index = s.index;
    this.saveSoon();
  }

  // ---- Audio ------------------------------------------------------------

  private ensureAudio(): HTMLAudioElement {
    if (this.audio) return this.audio;
    const a = new Audio();
    a.preload = 'auto';
    this.audio = a;
    this.applyVolume();
    a.addEventListener('timeupdate', () => {
      if (!this.loaded) return;
      this.position = this.offset + a.currentTime;
      this.saveSoon();
      const now = Date.now();
      if (now - this.lastPositionState > 1000) this.updatePositionState();
    });
    a.addEventListener('loadedmetadata', () => {
      if (this.pendingSeek !== null && this.loadedQuality === 'original') {
        a.currentTime = this.pendingSeek;
        this.pendingSeek = null;
      }
    });
    a.addEventListener('waiting', () => {
      if (this.playing) this.buffering = true;
    });
    a.addEventListener('playing', () => {
      this.buffering = false;
      if (a.paused || this.userPaused) {
        // A late event from a play() the user has since paused.
        if (!a.paused) a.pause();
        return;
      }
      this.playing = true;
      this.updatePositionState();
    });
    a.addEventListener('canplay', () => {
      if (a.paused) this.buffering = false;
    });
    // Pauses from outside (headset unplugged, another app taking audio).
    a.addEventListener('pause', () => {
      if (a.paused && !a.ended && this.playing) {
        this.playing = false;
        this.buffering = false;
        this.updatePositionState();
        this.saveSoon();
      }
    });
    a.addEventListener('ended', () => this.onEnded());
    a.addEventListener('error', () => void this.onError());
    this.installMediaSession();
    return a;
  }

  /** Loads the current entry at `start` seconds, and plays it if asked. */
  private load(start: number, autoplay: boolean): void {
    const entry = this.current;
    if (!entry) return;
    const audio = this.ensureAudio();
    this.cancelSkip();
    const quality = this.quality;
    const t = entry.track;
    const id = encodeURIComponent(t.fileId);
    const s = Math.max(0, Math.round(start * 10) / 10);
    if (quality === 'original') {
      this.offset = 0;
      this.pendingSeek = s > 0 ? s : null;
      audio.src = `/api/stream/${id}`;
    } else {
      this.offset = s;
      this.pendingSeek = null;
      audio.src = `/api/stream/${id}?quality=${quality}${s > 0 ? `&start=${s}` : ''}`;
    }
    this.loaded = true;
    this.loadedQuality = quality;
    this.position = s;
    this.playing = autoplay;
    this.buffering = autoplay;
    this.userPaused = !autoplay;
    this.updateMetadata();
    this.saveSoon();
    if (autoplay) this.startAudio();
  }

  private startAudio(): void {
    const audio = this.ensureAudio();
    audio.play().catch((err: unknown) => {
      // Autoplay refused (no user gesture yet), or a load replaced by the
      // next one. Media errors arrive through the "error" event instead.
      if (err instanceof DOMException && err.name === 'NotAllowedError') {
        this.playing = false;
        this.buffering = false;
      }
    });
  }

  /** Stops playback and unloads the stream, keeping the queue. */
  private stop(): void {
    this.playing = false;
    this.buffering = false;
    this.loaded = false;
    this.position = 0;
    if (this.audio) {
      this.audio.removeAttribute('src');
      this.audio.load();
    }
    this.updateMetadata();
    this.saveSoon();
  }

  private onEnded(): void {
    if (this.repeat === 'one') {
      this.load(0, true);
      return;
    }
    const i = nextIndex(this.queue.length, this.index, this.repeat);
    if (i === null) {
      // End of the queue: stay on the last track, ready to replay it.
      this.stop();
      return;
    }
    this.index = i;
    this.load(0, true);
  }

  private async onError(): Promise<void> {
    const audio = this.audio;
    if (!audio || !this.loaded || !audio.getAttribute('src')) return;
    const src = audio.src;
    const entry = this.current;
    this.buffering = false;
    const { status, message } = await diagnose(src);
    // Another track may have been loaded meanwhile.
    if (this.current !== entry || audio.src !== src) return;
    this.loaded = false;
    this.playing = false;
    if (status === 503) {
      this.notify(busyMessage);
      return;
    }
    this.notify(message ? `Lecture impossible : ${message}` : 'Lecture impossible.');
    const i = nextIndex(this.queue.length, this.index, this.repeat === 'one' ? 'off' : this.repeat);
    if (i === null || i === this.index) return;
    this.skipTimer = setTimeout(() => {
      this.skipTimer = undefined;
      if (this.current !== entry) return;
      this.index = i;
      this.load(0, true);
    }, skipAfterErrorMs);
  }

  private cancelSkip(): void {
    clearTimeout(this.skipTimer);
    this.skipTimer = undefined;
  }

  private applyVolume(): void {
    if (!this.audio) return;
    this.audio.volume = this.volume;
    this.audio.muted = this.muted;
  }

  private notify(text: string): void {
    clearTimeout(this.toastTimer);
    this.toast = { id: ++this.toastId, text };
    this.toastTimer = setTimeout(() => (this.toast = null), 4000);
  }

  // ---- Media Session ------------------------------------------------------

  private installMediaSession(): void {
    if (!('mediaSession' in navigator)) return;
    const ms = navigator.mediaSession;
    const handlers: [MediaSessionAction, MediaSessionActionHandler][] = [
      ['play', () => this.play()],
      ['pause', () => this.pause()],
      ['previoustrack', () => this.prev()],
      ['nexttrack', () => this.next()],
      ['seekto', (d) => d.seekTime !== undefined && this.seek(d.seekTime)],
      ['seekbackward', (d) => this.seekBy(-(d.seekOffset ?? seekStepSeconds))],
      ['seekforward', (d) => this.seekBy(d.seekOffset ?? seekStepSeconds)],
    ];
    for (const [action, handler] of handlers) {
      try {
        ms.setActionHandler(action, handler);
      } catch {
        // Action not supported by this browser.
      }
    }
  }

  private updateMetadata(): void {
    if (!('mediaSession' in navigator)) return;
    const t = this.current?.track;
    if (!t || !this.loaded) {
      navigator.mediaSession.metadata = null;
      navigator.mediaSession.playbackState = 'none';
      return;
    }
    const artwork = t.coverUrl
      ? [
          { src: trackCover(t, 250), sizes: '250x250', type: 'image/jpeg' },
          { src: trackCover(t, 500), sizes: '500x500', type: 'image/jpeg' },
        ]
      : [];
    try {
      navigator.mediaSession.metadata = new MediaMetadata({ title: t.title, artist: t.artist, album: t.album, artwork });
    } catch {
      // MediaMetadata missing or rejecting the artwork.
    }
    this.updatePositionState();
  }

  private updatePositionState(): void {
    this.lastPositionState = Date.now();
    if (!('mediaSession' in navigator)) return;
    navigator.mediaSession.playbackState = this.playing ? 'playing' : 'paused';
    const duration = this.duration;
    if (!(duration > 0) || !navigator.mediaSession.setPositionState) return;
    try {
      navigator.mediaSession.setPositionState({
        duration,
        position: Math.min(Math.max(0, this.position), duration),
        playbackRate: 1,
      });
    } catch {
      // Inconsistent values are rejected; the next update fixes them.
    }
  }

  // ---- Persistence ------------------------------------------------------

  private saveSoon(): void {
    if (this.saveTimer) return;
    const wait = Math.max(0, 1000 - (Date.now() - this.lastSave));
    this.saveTimer = setTimeout(() => this.saveNow(), wait);
  }

  private saveNow(): void {
    clearTimeout(this.saveTimer);
    this.saveTimer = undefined;
    this.lastSave = Date.now();
    const pos = new Map(this.queue.map((e, i) => [e.qid, i]));
    const data: Saved = {
      queue: this.queue.map((e) => e.track),
      original: this.original ? this.original.map((e) => pos.get(e.qid) ?? -1).filter((i) => i >= 0) : null,
      index: this.index,
      position: Math.round(this.position * 10) / 10,
      volume: this.volume,
      muted: this.muted,
      quality: this.quality,
      shuffle: this.shuffle,
      repeat: this.repeat,
    };
    try {
      localStorage.setItem(storageKey, JSON.stringify(data));
    } catch {
      // Storage full or disabled: the queue simply won't survive a reload.
    }
  }

  private restore(): void {
    this.quality = defaultQuality();
    let data: Partial<Saved> | null = null;
    try {
      const raw = localStorage.getItem(storageKey);
      data = raw ? (JSON.parse(raw) as Partial<Saved>) : null;
    } catch {
      data = null;
    }
    if (!data || typeof data !== 'object') return;
    if (isQuality(data.quality)) this.quality = data.quality;
    if (isRepeat(data.repeat)) this.repeat = data.repeat;
    if (typeof data.volume === 'number' && data.volume >= 0 && data.volume <= 1) this.volume = data.volume;
    this.muted = data.muted === true;
    if (!Array.isArray(data.queue)) return;
    const tracks = data.queue.filter(isTrack);
    if (tracks.length !== data.queue.length || tracks.length === 0) return;
    this.queue = this.entries(tracks);
    const index = typeof data.index === 'number' && Number.isInteger(data.index) ? data.index : 0;
    this.index = Math.min(Math.max(0, index), tracks.length - 1);
    const position = typeof data.position === 'number' && data.position > 0 ? data.position : 0;
    const max = (this.queue[this.index].track.durationMs || 0) / 1000;
    this.position = max > 0 ? Math.min(position, Math.max(0, max - 1)) : position;
    if (data.shuffle === true) {
      this.shuffle = true;
      const order = Array.isArray(data.original) ? data.original : [];
      const valid = order.length === tracks.length && new Set(order).size === tracks.length &&
        order.every((i) => Number.isInteger(i) && i >= 0 && i < tracks.length);
      this.original = valid ? order.map((i) => this.queue[i]) : this.queue.slice();
    }
  }
}

/** Reads why a stream failed: the HTTP status and the API's French message. */
async function diagnose(src: string): Promise<{ status: number; message: string }> {
  const controller = new AbortController();
  try {
    const res = await fetch(src, { signal: controller.signal, headers: { Range: 'bytes=0-0' } });
    if (res.ok) {
      // The stream itself is fine (a decoding or network problem): don't
      // download or convert it again.
      controller.abort();
      return { status: res.status, message: '' };
    }
    let message = '';
    try {
      const body: unknown = await res.json();
      if (body && typeof body === 'object' && 'error' in body && typeof body.error === 'string') {
        message = body.error;
      }
    } catch {
      // Not JSON.
    }
    return { status: res.status, message };
  } catch {
    return { status: 0, message: '' };
  }
}

export const player = new Player();
