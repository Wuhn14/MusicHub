import { execFileSync } from 'node:child_process';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { expect, test, type Page } from '@playwright/test';

// Recording "Glass Tide" of the invented MusicBrainz fixtures (internal/catalog/mbtest).
const recordingGlassTide = '00000000-0000-4000-8000-0000000000d1';
const albumPaperLanterns = '00000000-0000-4000-8000-0000000000b1';

/** Generates a short sine-wave MP3 (no real music) with the given tags. */
function makeMp3(dir: string, name: string, tags: Record<string, string>): string {
  const path = join(dir, name);
  const metadata = Object.entries(tags).flatMap(([k, v]) => ['-metadata', `${k}=${v}`]);
  execFileSync(
    process.env.MUSICHUB_FFMPEG || 'ffmpeg',
    [
      '-hide_banner', '-loglevel', 'error', '-y',
      '-f', 'lavfi', '-i', 'sine=frequency=392:duration=8',
      '-codec:a', 'libmp3lame', '-b:a', '64k',
      ...metadata,
      path,
    ],
    { stdio: ['ignore', 'ignore', 'inherit'] },
  );
  return path;
}

async function uploadThroughPage(page: Page, file: string) {
  await page.goto('/upload');
  const [chooser] = await Promise.all([
    page.waitForEvent('filechooser'),
    page.getByRole('button', { name: 'Choisir des fichiers' }).click(),
  ]);
  await chooser.setFiles(file);
}

test.beforeAll(async ({ request }) => {
  // Any key but the refused one: the fake AcoustID answers without matches
  // for the fingerprint of a generated sine wave.
  const res = await request.put('/api/settings', { data: { acoustidKey: 'cle-de-test' } });
  expect(res.ok()).toBeTruthy();
});

test('an unknown upload becomes a local track that plays', async ({ page, request }) => {
  const stamp = Date.now().toString(36);
  const dir = mkdtempSync(join(tmpdir(), 'musichub-e2e-flow-'));
  const name = `inconnu-${stamp}.mp3`;
  const title = `Mon enregistrement ${stamp}`;
  await uploadThroughPage(page, makeMp3(dir, name, {}));

  // The upload row only says « À identifier » after a two-minute window (the
  // page cannot tell a file waiting for a decision from one still being
  // checked), so the wait is on the server's own list instead.
  await expect(page.getByRole('listitem').filter({ hasText: name })).toBeVisible();
  await expect
    .poll(
      async () => {
        const list = (await (await request.get('/api/identify')).json()) as { files: { name: string }[] };
        return list.files.some((f) => f.name === name);
      },
      { timeout: 45_000 },
    )
    .toBe(true);

  await page.goto('/identify');
  const card = page.getByRole('article', { name });
  await card.getByRole('button', { name: 'Créer un titre local' }).click();
  const form = card.getByRole('form', { name: 'Créer un titre local' });
  await form.getByRole('textbox', { name: 'Titre', exact: true }).fill(title);
  await form.getByRole('textbox', { name: 'Artiste', exact: true }).fill('Moi');
  await form.getByRole('button', { name: 'Créer le titre local' }).click();
  await expect(card).toHaveCount(0);

  await page.goto('/library?tab=tracks');
  await page.getByPlaceholder('Filtrer les titres…').fill(title);
  const track = page.getByRole('listitem').filter({ hasText: title });
  await expect(track.getByText('local', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: `Lire ${title}` }).click();

  const bar = page.getByRole('contentinfo', { name: 'Lecteur' });
  await expect(bar.getByText(title)).toBeVisible();
  const slider = bar.getByRole('slider', { name: 'Position dans le titre' });
  await expect.poll(async () => Number(await slider.inputValue()), { timeout: 15_000 }).toBeGreaterThanOrEqual(1);
});

test('a file tagged with a MusicBrainz recording id is identified by itself', async ({ page, request }) => {
  const stamp = Date.now().toString(36);
  const dir = mkdtempSync(join(tmpdir(), 'musichub-e2e-flow-'));
  const name = `etiquete-${stamp}.mp3`;
  await uploadThroughPage(
    page,
    makeMp3(dir, name, {
      MUSICBRAINZ_TRACKID: recordingGlassTide,
      artist: 'Lumen Harbor',
      title: 'Glass Tide',
    }),
  );

  const row = page.getByRole('listitem').filter({ hasText: name });
  await expect(row.getByText(/Identifié : .*Glass Tide/)).toBeVisible({ timeout: 45_000 });

  // The file is in the library under the fixture album, and can be played.
  await page.goto('/library?tab=tracks');
  await page.getByPlaceholder('Filtrer les titres…').fill('Glass Tide');
  await expect(page.getByRole('button', { name: 'Lire Glass Tide' }).first()).toBeVisible();

  // The catalog knows the recording as owned: the availability answer (not
  // mocked here) maps it to a library file.
  const res = await request.get(`/api/library/availability?album=${albumPaperLanterns}`);
  expect(res.ok()).toBeTruthy();
  const body = (await res.json()) as { tracks: Record<string, string> };
  expect(Object.keys(body.tracks)).toContain(recordingGlassTide);
});
