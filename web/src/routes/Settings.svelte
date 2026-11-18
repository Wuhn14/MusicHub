<script lang="ts">
  import {
    errorMessage,
    getSettings,
    getSystem,
    isAbort,
    saveSettings,
    type Settings,
    type ToolStatus,
  } from '../lib/api';
  import Status from '../components/Status.svelte';

  const MiB = 1 << 20;
  const GiB = 1 << 30;

  // What each external tool is needed for, shown when it is missing.
  const toolRoles: Record<string, { label: string; missing: string }> = {
    ffprobe: { label: 'ffprobe', missing: 'Sans lui, l’analyse des fichiers audio est indisponible.' },
    ffmpeg: {
      label: 'ffmpeg',
      missing: 'Sans lui, la conversion et l’écriture des tags sont indisponibles.',
    },
    fpcalc: { label: 'fpcalc', missing: 'Sans lui, la reconnaissance par empreinte est indisponible.' },
  };

  let loading = $state(true);
  let loadError = $state('');
  let saving = $state(false);
  let saveError = $state('');
  let saved = $state(false);

  let tools = $state<ToolStatus[]>([]);
  let toolsError = $state('');

  // Form state.
  let maskedKey = $state('');
  let keySet = $state(false);
  let newKey = $state('');
  let scanInterval = $state(15);
  let organizeDropped = $state(false);
  let writeTagsUploaded = $state(true);
  let writeTagsDropped = $state(false);
  let duplicates = $state<Settings['duplicates']>('best');
  let uploadSize = $state(2);
  let uploadUnit = $state<'Mo' | 'Go'>('Go');
  let maxTranscodes = $state(3);

  function apply(s: Settings): void {
    maskedKey = s.acoustidKey;
    keySet = s.acoustidKeySet;
    newKey = '';
    scanInterval = s.scanIntervalMinutes;
    organizeDropped = s.organizeDropped;
    writeTagsUploaded = s.writeTagsUploaded;
    writeTagsDropped = s.writeTagsDropped;
    duplicates = s.duplicates;
    maxTranscodes = s.maxTranscodes;
    if (s.maxUploadBytes >= GiB && s.maxUploadBytes % GiB === 0) {
      uploadSize = s.maxUploadBytes / GiB;
      uploadUnit = 'Go';
    } else {
      uploadSize = Math.round(s.maxUploadBytes / MiB);
      uploadUnit = 'Mo';
    }
  }

  $effect(() => {
    const controller = new AbortController();
    getSettings(controller.signal)
      .then(apply)
      .catch((err) => {
        if (!isAbort(err)) {
          loadError = errorMessage(err);
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          loading = false;
        }
      });
    getSystem(controller.signal)
      .then((t) => (tools = t))
      .catch((err) => {
        if (!isAbort(err)) {
          toolsError = errorMessage(err);
        }
      });
    return () => controller.abort();
  });

  async function save(deleteKey = false): Promise<void> {
    saving = true;
    saved = false;
    saveError = '';
    try {
      const unit = uploadUnit === 'Go' ? GiB : MiB;
      const patch: Parameters<typeof saveSettings>[0] = {
        scanIntervalMinutes: Number(scanInterval),
        organizeDropped,
        writeTagsUploaded,
        writeTagsDropped,
        duplicates,
        maxUploadBytes: Math.round(Number(uploadSize) * unit),
        maxTranscodes: Number(maxTranscodes),
      };
      // The key is only sent when the user typed one, or asked to remove it.
      if (deleteKey) {
        patch.acoustidKey = '';
      } else if (newKey.trim() !== '') {
        patch.acoustidKey = newKey.trim();
      }
      apply(await saveSettings(patch));
      saved = true;
    } catch (err) {
      saveError = errorMessage(err);
    }
    saving = false;
  }

  function onsubmit(event: SubmitEvent): void {
    event.preventDefault();
    void save();
  }
</script>

<svelte:head>
  <title>Réglages · MusicHub</title>
</svelte:head>

<section class="settings">
  <h1>Réglages</h1>

  <Status {loading} error={loadError} />

  {#if !loading && !loadError}
    <form {onsubmit}>
      <fieldset>
        <legend>Identification</legend>
        <div class="field">
          <label for="acoustid">Clé AcoustID</label>
          <div class="inline">
            <input
              id="acoustid"
              type="password"
              autocomplete="off"
              spellcheck="false"
              placeholder={keySet ? maskedKey : 'Aucune clé enregistrée'}
              bind:value={newKey}
            />
            {#if keySet}
              <button type="button" class="secondary" disabled={saving} onclick={() => save(true)}>
                Supprimer la clé
              </button>
            {/if}
          </div>
          <p class="hint">
            La clé est gratuite : tu peux la créer sur le site d’AcoustID. Elle sert à reconnaître les
            morceaux par leur empreinte sonore. Laisse le champ vide pour garder la clé actuelle.
          </p>
        </div>
      </fieldset>

      <fieldset>
        <legend>Bibliothèque</legend>
        <div class="field">
          <label for="scan">Intervalle d’analyse (minutes)</label>
          <input id="scan" type="number" min="1" max="1440" step="1" required bind:value={scanInterval} />
          <p class="hint">Entre 1 minute et 24 heures.</p>
        </div>
        <label class="check">
          <input type="checkbox" bind:checked={organizeDropped} />
          <span>Ranger les fichiers déposés dans la bibliothèque</span>
        </label>
        <label class="check">
          <input type="checkbox" bind:checked={writeTagsUploaded} />
          <span>Écrire les tags des fichiers envoyés</span>
        </label>
        <label class="check">
          <input type="checkbox" bind:checked={writeTagsDropped} />
          <span>Écrire les tags des fichiers déposés</span>
        </label>
        <div class="field">
          <label for="dups">Doublons</label>
          <select id="dups" bind:value={duplicates}>
            <option value="best">Garder la meilleure qualité</option>
            <option value="keep-both">Garder les deux</option>
            <option value="keep-existing">Garder l’existant</option>
          </select>
        </div>
      </fieldset>

      <fieldset>
        <legend>Envois</legend>
        <div class="field">
          <label for="upload">Taille maximale d’un envoi</label>
          <div class="inline">
            <input
              id="upload"
              class="short"
              type="number"
              min="1"
              step="any"
              required
              bind:value={uploadSize}
            />
            <select aria-label="Unité" bind:value={uploadUnit}>
              <option value="Mo">Mo</option>
              <option value="Go">Go</option>
            </select>
          </div>
          <p class="hint">Entre 1 Mo et 20 Go.</p>
        </div>
      </fieldset>

      <fieldset>
        <legend>Lecture</legend>
        <div class="field">
          <label for="transcodes">Conversions simultanées</label>
          <input
            id="transcodes"
            class="short"
            type="number"
            min="1"
            max="16"
            step="1"
            required
            bind:value={maxTranscodes}
          />
          <p class="hint">Entre 1 et 16. Une conversion sollicite le processeur du serveur.</p>
        </div>
      </fieldset>

      <div class="actions">
        <button type="submit" class="primary" disabled={saving}>
          {saving ? 'Enregistrement…' : 'Enregistrer'}
        </button>
        {#if saved}
          <p class="ok" role="status">Réglages enregistrés.</p>
        {/if}
      </div>
      {#if saveError}
        <Status error={saveError} />
      {/if}
    </form>
  {/if}

  <section class="tools" aria-labelledby="tools-title">
    <h2 id="tools-title">Outils audio</h2>
    {#if toolsError}
      <Status error={toolsError} />
    {:else}
      <ul>
        {#each tools as tool (tool.name)}
          {@const role = toolRoles[tool.name]}
          <li>
            <div class="tool-head">
              <span class="tool-name">{role?.label ?? tool.name}</span>
              <span class="badge" class:ok={tool.available} class:ko={!tool.available}>
                {tool.available ? 'Disponible' : 'Manquant'}
              </span>
            </div>
            {#if tool.available}
              <p class="hint">{tool.version}</p>
            {:else if role}
              <p class="hint">{role.missing}</p>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </section>
</section>

<style>
  .settings {
    max-width: 720px;
  }

  h1 {
    margin-bottom: 24px;
    font-size: 2rem;
    font-weight: 800;
    letter-spacing: -0.02em;
  }

  h2 {
    margin-bottom: 12px;
    font-size: 1.15rem;
    font-weight: 700;
  }

  form {
    display: flex;
    flex-direction: column;
    gap: 20px;
  }

  fieldset {
    display: flex;
    flex-direction: column;
    gap: 16px;
    min-width: 0;
    margin: 0;
    padding: 16px 20px 20px;
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    background: var(--bg-elevated);
  }

  legend {
    padding: 0 8px;
    font-weight: 700;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  label {
    font-weight: 600;
  }

  .inline {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  input[type='password'],
  input[type='number'],
  select {
    min-width: 0;
    padding: 9px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
  }

  input[type='password'] {
    flex: 1 1 200px;
  }

  input[type='number'] {
    width: 100%;
    max-width: 200px;
  }

  input.short {
    width: 120px;
  }

  select {
    max-width: 100%;
  }

  .check {
    display: flex;
    align-items: center;
    gap: 10px;
    font-weight: 500;
  }

  .check input {
    flex: none;
    width: 18px;
    height: 18px;
    accent-color: var(--accent);
  }

  .hint {
    font-size: 0.85rem;
    color: var(--text-muted);
  }

  .actions {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 16px;
  }

  button {
    padding: 10px 20px;
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

  .primary:hover:not(:disabled) {
    background: var(--accent-strong);
  }

  .secondary {
    background: var(--surface-hover);
    color: var(--text);
  }

  .secondary:hover:not(:disabled) {
    color: var(--danger);
  }

  p.ok {
    color: var(--accent-strong);
    font-weight: 600;
  }

  .tools {
    margin-top: 32px;
  }

  .tools ul {
    display: flex;
    flex-direction: column;
    gap: 12px;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .tools li {
    padding: 12px 16px;
    border-radius: var(--radius);
    background: var(--bg-elevated);
  }

  .tool-head {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .tool-name {
    font-family: ui-monospace, 'Cascadia Mono', Consolas, monospace;
    font-weight: 700;
  }

  .badge {
    padding: 2px 10px;
    border-radius: 999px;
    font-size: 0.8rem;
    font-weight: 700;
  }

  .badge.ok {
    background: rgb(255 149 72 / 0.15);
    color: var(--accent-strong);
  }

  .badge.ko {
    background: rgb(255 107 107 / 0.12);
    color: var(--danger);
  }
</style>
