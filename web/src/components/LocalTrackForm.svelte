<script lang="ts">
  // Describes a file MusicBrainz does not know as a "titre local": title and
  // artist are required, the rest (and a cover image) is optional.
  import { onDestroy } from 'svelte';
  import { createLocalTrack, errorMessage, uploadLocalCover, type IdentifyTags } from '../lib/api';

  interface Props {
    fileId: string;
    /** Tags read from the file, used to prefill the form. */
    tags: IdentifyTags;
    /** Called once the local track exists; coverError is '' when all went well. */
    oncreated: (coverError: string) => void;
    oncancel: () => void;
  }

  let { fileId, tags, oncreated, oncancel }: Props = $props();

  const maxCoverBytes = 5 * 1024 * 1024;
  const coverTypes = ['image/jpeg', 'image/png', 'image/webp'];

  // Prefilled once from the tags; the user edits them afterwards.
  const initial = (() => ({ ...tags }))();
  let title = $state(initial.title);
  let artist = $state(initial.artist);
  let album = $state(initial.album);
  let albumArtist = $state(initial.albumArtist);
  let disc = $state<number | string | null>(initial.disc > 0 ? initial.disc : '');
  let position = $state<number | string | null>(initial.track > 0 ? initial.track : '');
  let year = $state<number | string | null>(initial.year);

  let cover = $state<File | null>(null);
  let preview = $state('');
  let coverProblem = $state('');
  let error = $state('');
  let busy = $state(false);

  const uid = $derived(`local-${fileId}`);

  function clearPreview(): void {
    if (preview) {
      URL.revokeObjectURL(preview);
      preview = '';
    }
  }

  onDestroy(clearPreview);

  function pickCover(e: Event): void {
    const input = e.currentTarget as HTMLInputElement;
    const file = input.files?.[0] ?? null;
    clearPreview();
    coverProblem = '';
    cover = null;
    if (!file) {
      return;
    }
    if (file.type && !coverTypes.includes(file.type)) {
      coverProblem = 'Image non prise en charge (JPEG, PNG ou WebP).';
      input.value = '';
      return;
    }
    if (file.size > maxCoverBytes) {
      coverProblem = 'Image trop volumineuse (5 Mo au maximum).';
      input.value = '';
      return;
    }
    cover = file;
    preview = URL.createObjectURL(file);
  }

  function removeCover(): void {
    clearPreview();
    cover = null;
    coverProblem = '';
  }

  // Number inputs bind numbers once edited (null when cleared), strings before.
  function toNumber(v: unknown): number {
    const n = typeof v === 'number' ? v : Number.parseInt(String(v ?? '').trim(), 10);
    return Number.isFinite(n) ? Math.trunc(n) : 0;
  }

  async function submit(e: SubmitEvent): Promise<void> {
    e.preventDefault();
    error = '';
    if (!title.trim() || !artist.trim()) {
      error = 'Le titre et l’artiste sont obligatoires.';
      return;
    }
    busy = true;
    try {
      await createLocalTrack(fileId, {
        title: title.trim(),
        artist: artist.trim(),
        album: album.trim(),
        albumArtist: albumArtist.trim(),
        disc: toNumber(disc),
        position: toNumber(position),
        year: toNumber(year),
      });
    } catch (err) {
      error = errorMessage(err);
      busy = false;
      return;
    }
    let coverError = '';
    if (cover) {
      try {
        await uploadLocalCover(fileId, cover);
      } catch (err) {
        coverError = `Titre créé, mais la pochette n’a pas été enregistrée : ${errorMessage(err)}`;
      }
    }
    busy = false;
    oncreated(coverError);
  }
</script>

<form class="form" onsubmit={submit} aria-label="Créer un titre local" novalidate>
  <div class="grid">
    <div class="field wide">
      <label for="{uid}-title">Titre <span aria-hidden="true">*</span></label>
      <input id="{uid}-title" required maxlength="300" bind:value={title} autocomplete="off" />
    </div>
    <div class="field">
      <label for="{uid}-artist">Artiste <span aria-hidden="true">*</span></label>
      <input id="{uid}-artist" required maxlength="300" bind:value={artist} autocomplete="off" />
    </div>
    <div class="field">
      <label for="{uid}-album-artist">Artiste de l’album</label>
      <input id="{uid}-album-artist" maxlength="300" bind:value={albumArtist} placeholder={artist} autocomplete="off" />
    </div>
    <div class="field wide">
      <label for="{uid}-album">Album</label>
      <input id="{uid}-album" maxlength="300" bind:value={album} autocomplete="off" />
    </div>
    <div class="field small">
      <label for="{uid}-disc">Disque</label>
      <input id="{uid}-disc" type="number" inputmode="numeric" min="1" max="99" bind:value={disc} />
    </div>
    <div class="field small">
      <label for="{uid}-track">Piste</label>
      <input id="{uid}-track" type="number" inputmode="numeric" min="1" max="999" bind:value={position} />
    </div>
    <div class="field small">
      <label for="{uid}-year">Année</label>
      <input id="{uid}-year" type="number" inputmode="numeric" min="1000" max="9999" bind:value={year} />
    </div>
  </div>

  <div class="cover">
    {#if preview}
      <img src={preview} alt="Aperçu de la pochette" width="72" height="72" />
    {:else}
      <div class="placeholder" aria-hidden="true"></div>
    {/if}
    <div class="cover-actions">
      <label class="file">
        <input type="file" accept="image/jpeg,image/png,image/webp" onchange={pickCover} />
        <span>{cover ? 'Changer la pochette' : 'Ajouter une pochette'}</span>
      </label>
      {#if cover}
        <button type="button" class="link" onclick={removeCover}>Retirer</button>
      {/if}
      <p class="hint">JPEG, PNG ou WebP, 5 Mo au maximum.</p>
      {#if coverProblem}
        <p class="bad" role="alert">{coverProblem}</p>
      {/if}
    </div>
  </div>

  {#if error}
    <p class="bad" role="alert">{error}</p>
  {/if}

  <div class="actions">
    <button type="submit" class="primary" disabled={busy}>{busy ? 'Création…' : 'Créer le titre local'}</button>
    <button type="button" class="secondary" onclick={oncancel} disabled={busy}>Annuler</button>
  </div>
</form>

<style>
  .form {
    display: flex;
    flex-direction: column;
    gap: 14px;
    padding: 16px;
    border-radius: var(--radius);
    background: var(--surface);
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(6, minmax(0, 1fr));
    gap: 12px;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    grid-column: span 3;
  }

  .field.wide {
    grid-column: span 6;
  }

  .field.small {
    grid-column: span 2;
  }

  label {
    font-size: 0.82rem;
    font-weight: 600;
    color: var(--text-muted);
  }

  input:not([type='file']) {
    min-height: 40px;
    padding: 8px 10px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg);
  }

  input:not([type='file']):focus-visible {
    border-color: var(--accent);
    outline: none;
  }

  .cover {
    display: flex;
    align-items: center;
    gap: 14px;
  }

  .cover img,
  .placeholder {
    flex: none;
    width: 72px;
    height: 72px;
    border-radius: 6px;
    object-fit: cover;
  }

  .placeholder {
    border: 1px dashed var(--border);
    background: var(--bg);
  }

  .cover-actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px 12px;
  }

  .file {
    position: relative;
    display: inline-flex;
    align-items: center;
    min-height: 36px;
    padding: 6px 14px;
    border: 1px solid var(--border);
    border-radius: 999px;
    color: var(--text);
    font-weight: 600;
    cursor: pointer;
  }

  .file:focus-within {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .file input {
    position: absolute;
    inset: 0;
    opacity: 0;
    cursor: pointer;
  }

  .hint {
    width: 100%;
    font-size: 0.78rem;
    color: var(--text-faint);
  }

  .bad {
    color: var(--danger);
    font-size: 0.9rem;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
  }

  button {
    min-height: 40px;
    padding: 8px 18px;
    border: 0;
    border-radius: 999px;
    font-weight: 700;
    cursor: pointer;
  }

  button:disabled {
    opacity: 0.6;
    cursor: default;
  }

  .primary {
    background: var(--accent);
    color: var(--on-accent);
  }

  .secondary {
    background: transparent;
    border: 1px solid var(--border);
  }

  .link {
    min-height: 0;
    padding: 0;
    background: none;
    color: var(--text-muted);
    font-weight: 600;
    text-decoration: underline;
  }

  @media (max-width: 560px) {
    .field,
    .field.wide {
      grid-column: span 6;
    }

    .field.small {
      grid-column: span 2;
    }
  }
</style>
