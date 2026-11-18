<script lang="ts">
  import { onDestroy } from 'svelte';
  import {
    acceptAttribute,
    extensionOf,
    filesFromDrop,
    getFileStatus,
    isAllowedName,
    maxConcurrentUploads,
    reasonFromMessage,
    uploadFile,
    UploadError,
    type UploadHandle,
  } from '../lib/upload';

  type State = 'queued' | 'uploading' | 'sent' | 'refused' | 'cancelled';
  type Ident = 'checking' | 'identified' | 'unidentified';

  interface Item {
    key: number;
    name: string;
    state: State;
    progress: number;
    reason: string;
    fileId: string;
    ident: Ident;
    identified: string;
    sentAt: number;
  }

  /** The identification check stops after this long; the file then reads "À identifier". */
  const pollWindowMs = 2 * 60 * 1000;
  const pollEveryMs = 3000;

  let items = $state<Item[]>([]);
  let dragging = $state(false);
  let nextKey = 1;

  // Not reactive: files and request handles.
  const files = new Map<number, File>();
  const handles = new Map<number, UploadHandle>();
  let active = 0;

  let filePicker: HTMLInputElement;
  let folderPicker: HTMLInputElement;

  const busy = $derived(items.some((i) => i.state === 'queued' || i.state === 'uploading'));

  function add(list: File[]): void {
    for (const file of list) {
      const item: Item = {
        key: nextKey++,
        name: file.name,
        state: 'queued',
        progress: 0,
        reason: '',
        fileId: '',
        ident: 'checking',
        identified: '',
        sentAt: 0,
      };
      if (!isAllowedName(file.name)) {
        const ext = extensionOf(file.name);
        item.state = 'refused';
        item.reason = ext ? `format ${ext} non pris en charge` : 'format non pris en charge';
      } else {
        files.set(item.key, file);
      }
      items.push(item);
    }
    pump();
  }

  function find(key: number): Item | undefined {
    return items.find((i) => i.key === key);
  }

  function pump(): void {
    while (active < maxConcurrentUploads) {
      const next = items.find((i) => i.state === 'queued');
      if (!next) {
        return;
      }
      void start(next.key);
    }
  }

  async function start(key: number): Promise<void> {
    const file = files.get(key);
    const item = find(key);
    if (!file || !item) {
      return;
    }
    active++;
    item.state = 'uploading';
    const handle = uploadFile(file, (f) => {
      const it = find(key);
      if (it) {
        it.progress = f;
      }
    });
    handles.set(key, handle);
    try {
      const sent = await handle.promise;
      const it = find(key);
      if (it) {
        it.state = 'sent';
        it.progress = 1;
        it.fileId = sent.id;
        it.sentAt = Date.now();
      }
    } catch (err) {
      const it = find(key);
      if (it) {
        if (err instanceof UploadError && err.aborted) {
          it.state = 'cancelled';
        } else {
          it.state = 'refused';
          it.reason = reasonFromMessage(err instanceof UploadError ? err.message : 'envoi impossible');
        }
      }
    } finally {
      handles.delete(key);
      files.delete(key);
      active--;
      pump();
    }
  }

  function cancel(item: Item): void {
    if (item.state === 'queued') {
      item.state = 'cancelled';
      files.delete(item.key);
    } else if (item.state === 'uploading') {
      handles.get(item.key)?.abort();
    }
  }

  // Identification poll: every 3 s, for up to 2 min per file.
  const timer = setInterval(() => void poll(), pollEveryMs);
  onDestroy(() => clearInterval(timer));
  let polling = false;

  async function poll(): Promise<void> {
    if (polling) {
      return;
    }
    polling = true;
    try {
      for (const item of items) {
        if (item.state !== 'sent' || item.ident !== 'checking') {
          continue;
        }
        try {
          const s = await getFileStatus(item.fileId);
          if (s.status === 'identified' || s.status === 'local') {
            item.ident = 'identified';
            item.identified = [s.title, s.artist].filter(Boolean).join(' — ');
            continue;
          }
          if (s.status !== 'pending') {
            item.ident = 'unidentified';
            continue;
          }
        } catch {
          // A failed check is retried at the next tick, until the window ends.
        }
        if (Date.now() - item.sentAt > pollWindowMs) {
          item.ident = 'unidentified';
        }
      }
    } finally {
      polling = false;
    }
  }

  // Leaving the page would cancel the running uploads.
  $effect(() => {
    if (!busy) {
      return;
    }
    const warn = (e: BeforeUnloadEvent) => {
      e.preventDefault();
      e.returnValue = '';
    };
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  });

  function picked(e: Event): void {
    const input = e.currentTarget as HTMLInputElement;
    add(Array.from(input.files ?? []));
    input.value = ''; // the same file can be chosen again
  }

  async function dropped(e: DragEvent): Promise<void> {
    e.preventDefault();
    dragging = false;
    if (!e.dataTransfer) {
      return;
    }
    const fallback = Array.from(e.dataTransfer.files);
    const found = await filesFromDrop(e.dataTransfer);
    add(found.length > 0 ? found : fallback);
  }

  function over(e: DragEvent): void {
    e.preventDefault();
    dragging = true;
  }
</script>

<section class="upload">
  <h1>Ajouter de la musique</h1>

  <div
    class="zone"
    class:dragging
    role="group"
    aria-label="Zone de dépôt de fichiers"
    ondragover={over}
    ondragleave={() => (dragging = false)}
    ondrop={dropped}
  >
    <p class="lead">Glisse des fichiers ou un dossier ici</p>
    <p class="hint">MP3, FLAC, M4A, AAC, OGG, Opus, WAV, AIFF. Ils seront identifiés automatiquement.</p>
    <div class="buttons">
      <button type="button" class="primary" onclick={() => filePicker.click()}>Choisir des fichiers</button>
      <button type="button" class="secondary" onclick={() => folderPicker.click()}>Choisir un dossier</button>
    </div>
    <input
      bind:this={filePicker}
      class="picker"
      type="file"
      multiple
      accept={acceptAttribute}
      aria-label="Fichiers audio"
      tabindex="-1"
      onchange={picked}
    />
    <input
      bind:this={folderPicker}
      class="picker"
      type="file"
      multiple
      webkitdirectory
      aria-label="Dossier de musique"
      tabindex="-1"
      onchange={picked}
    />
  </div>

  {#if items.length > 0}
    <ul class="list" aria-label="Fichiers envoyés">
      {#each items as item (item.key)}
        <li class="item" class:refused={item.state === 'refused'}>
          <div class="row">
            <span class="name" title={item.name}>{item.name}</span>
            {#if item.state === 'queued' || item.state === 'uploading'}
              <button type="button" class="cancel" aria-label={`Annuler l’envoi de ${item.name}`} onclick={() => cancel(item)}>
                Annuler
              </button>
            {/if}
          </div>
          {#if item.state === 'queued'}
            <p class="state">En attente…</p>
          {:else if item.state === 'uploading'}
            <progress max="1" value={item.progress} aria-label={`Progression de ${item.name}`}></progress>
            <p class="state">Envoi en cours… {Math.round(item.progress * 100)} %</p>
          {:else if item.state === 'sent'}
            {#if item.ident === 'identified'}
              <p class="state done">Identifié : {item.identified}</p>
            {:else if item.ident === 'unidentified'}
              <p class="state"><a href="/identify">À identifier</a></p>
            {:else}
              <p class="state">Envoyé — identification en cours</p>
            {/if}
          {:else if item.state === 'refused'}
            <p class="state bad">Refusé : {item.reason}</p>
          {:else}
            <p class="state">Annulé</p>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  .upload {
    max-width: 760px;
  }

  h1 {
    margin-bottom: 24px;
    font-size: 2rem;
    font-weight: 800;
    letter-spacing: -0.02em;
  }

  .zone {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
    padding: 32px 20px;
    border: 2px dashed var(--border);
    border-radius: var(--radius-lg);
    background: var(--bg-elevated);
    text-align: center;
    transition: border-color 0.15s ease, background-color 0.15s ease;
  }

  .zone.dragging {
    border-color: var(--accent);
    background: var(--surface);
  }

  .lead {
    font-size: 1.15rem;
    font-weight: 700;
  }

  .hint {
    font-size: 0.85rem;
    color: var(--text-muted);
  }

  .buttons {
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    gap: 12px;
    margin-top: 8px;
  }

  button {
    min-height: 48px;
    padding: 12px 24px;
    border: 0;
    border-radius: 999px;
    font-weight: 700;
    cursor: pointer;
  }

  .primary {
    background: var(--accent);
    color: var(--on-accent);
  }

  .secondary {
    background: var(--surface);
    color: var(--text);
    border: 1px solid var(--border);
  }

  /* Hidden but still in the DOM, so test tools and screen readers can reach them. */
  .picker {
    position: absolute;
    width: 1px;
    height: 1px;
    opacity: 0;
    overflow: hidden;
    pointer-events: none;
  }

  .list {
    list-style: none;
    margin: 24px 0 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .item {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 12px 14px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg-elevated);
  }

  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 600;
  }

  .cancel {
    flex: none;
    min-height: 44px;
    padding: 8px 16px;
    background: transparent;
    color: var(--text-muted);
    border: 1px solid var(--border);
  }

  progress {
    width: 100%;
    height: 8px;
    accent-color: var(--accent);
  }

  .state {
    font-size: 0.9rem;
    color: var(--text-muted);
  }

  .state.done {
    color: var(--accent-strong);
  }

  .state.bad {
    color: var(--danger);
  }

  .state a {
    color: var(--accent-strong);
    font-weight: 600;
  }

  @media (max-width: 760px) {
    .zone {
      padding: 24px 16px;
    }

    .buttons button {
      flex: 1 1 100%;
    }
  }
</style>
