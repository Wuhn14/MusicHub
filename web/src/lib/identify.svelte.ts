// Shared count of the files waiting in "À identifier", shown as a badge in
// the menu. It is polled every 30 seconds and refreshed after each action.

import { getIdentifyCount } from './api';

const pollMs = 30_000;

class IdentifyCount {
  /** Files waiting for the user. */
  count = $state(0);
  /** Files still being identified automatically. */
  queued = $state(0);

  #timer: ReturnType<typeof setInterval> | undefined;
  #users = 0;
  #inflight = false;

  /** Reads the count again now. Failures keep the last known value. */
  async refresh(): Promise<void> {
    if (this.#inflight) {
      return;
    }
    this.#inflight = true;
    try {
      const c = await getIdentifyCount();
      this.count = c.count;
      this.queued = c.queued;
    } catch {
      // Keep the badge as it was; the next poll tries again.
    } finally {
      this.#inflight = false;
    }
  }

  /** Starts polling while at least one component listens; returns a stop function. */
  watch(): () => void {
    this.#users++;
    if (this.#users === 1) {
      void this.refresh();
      this.#timer = setInterval(() => void this.refresh(), pollMs);
    }
    return () => {
      this.#users--;
      if (this.#users === 0 && this.#timer !== undefined) {
        clearInterval(this.#timer);
        this.#timer = undefined;
      }
    };
  }
}

export const identifyCount = new IdentifyCount();
