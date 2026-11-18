import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { expect, test, type APIRequestContext } from '@playwright/test';

// Generates a short invented MP3 (ffmpeg sine wave); no real music is used.
// The fake MusicBrainz proposes "Glass Tide" for any title search, but the
// two-second length never matches it, so the file waits in "À identifier".
function makeMp3(dir: string, name: string, title: string): Buffer {
  const path = join(dir, name);
  execFileSync(
    process.env.MUSICHUB_FFMPEG || 'ffmpeg',
    [
      '-hide_banner', '-loglevel', 'error', '-y',
      '-f', 'lavfi', '-i', 'sine=frequency=550:duration=2',
      '-codec:a', 'libmp3lame', '-b:a', '64k',
      '-metadata', `title=${title}`,
      '-metadata', 'artist=Artiste Imaginaire',
      path,
    ],
    { stdio: ['ignore', 'ignore', 'inherit'] },
  );
  return readFileSync(path);
}

async function upload(request: APIRequestContext, name: string, data: Buffer): Promise<string> {
  const res = await request.post('/api/uploads', {
    headers: { 'X-File-Name': encodeURIComponent(name), 'Content-Type': 'application/octet-stream' },
    data,
  });
  expect(res.status()).toBe(201);
  return ((await res.json()) as { id: string }).id;
}

/** Waits until the automatic identification has given up on every file. */
async function waitAwaiting(request: APIRequestContext, ids: string[]): Promise<void> {
  await expect
    .poll(
      async () => {
        const list = (await (await request.get('/api/identify')).json()) as { files: { id: string }[] };
        return ids.every((id) => list.files.some((f) => f.id === id));
      },
      { timeout: 30_000 },
    )
    .toBe(true);
}

test('identify files: pick a candidate, create a local track, delete an upload', async ({ page, request }) => {
  const dir = mkdtempSync(join(tmpdir(), 'musichub-e2e-identify-'));
  const stamp = Date.now().toString(36);
  const chosenName = `choix-${stamp}.mp3`;
  const localName = `local-${stamp}.mp3`;
  const deletedName = `jeter-${stamp}.mp3`;
  const ids = [
    await upload(request, chosenName, makeMp3(dir, 'a.mp3', 'Marée Haute')),
    await upload(request, localName, makeMp3(dir, 'b.mp3', 'Maquette Douce')),
    await upload(request, deletedName, makeMp3(dir, 'c.mp3', 'Brouillon')),
  ];
  await waitAwaiting(request, ids);

  await page.goto('/');
  const navLink = page.getByRole('navigation', { name: 'Navigation principale' }).getByRole('link', { name: /À identifier/ });
  await expect(navLink).toHaveAccessibleName(/À identifier, \d+ fichiers?/);
  await navLink.click();
  await expect(page).toHaveURL(/\/identify$/);
  await expect(page.getByRole('heading', { level: 1, name: 'À identifier' })).toBeVisible();

  // 1. Pick a proposed recording.
  const chosen = page.getByRole('article', { name: chosenName });
  await expect(chosen.getByText('Artiste Imaginaire · Marée Haute')).toBeVisible();
  await expect(chosen.getByRole('button', { name: `Écouter ${chosenName}` })).toBeVisible();
  const candidate = chosen.getByRole('listitem').filter({ hasText: 'Glass Tide — Lumen Harbor' });
  await expect(candidate.getByText('100 %')).toBeVisible();
  await candidate.getByRole('button', { name: 'Choisir Glass Tide — Lumen Harbor' }).click();
  await expect(chosen).toHaveCount(0);

  // 2. Describe one as a local track, prefilled from its tags.
  const local = page.getByRole('article', { name: localName });
  await local.getByRole('button', { name: 'Créer un titre local' }).click();
  const form = local.getByRole('form', { name: 'Créer un titre local' });
  await expect(form.getByRole('textbox', { name: 'Titre', exact: true })).toHaveValue('Maquette Douce');
  await expect(form.getByRole('textbox', { name: 'Artiste', exact: true })).toHaveValue('Artiste Imaginaire');
  await form.getByRole('textbox', { name: 'Titre', exact: true }).fill('');
  await form.getByRole('button', { name: 'Créer le titre local' }).click();
  await expect(form.getByText('Le titre et l’artiste sont obligatoires.')).toBeVisible();
  await form.getByRole('textbox', { name: 'Titre', exact: true }).fill('Maquette Douce');
  await form.getByRole('textbox', { name: 'Album', exact: true }).fill(`Démos ${stamp}`);
  await form.getByRole('spinbutton', { name: 'Piste' }).fill('2');
  await form.getByRole('spinbutton', { name: 'Année' }).fill('2024');
  await form.getByRole('button', { name: 'Créer le titre local' }).click();
  await expect(local).toHaveCount(0);

  // 3. Delete an uploaded file, after confirming.
  const doomed = page.getByRole('article', { name: deletedName });
  await doomed.getByRole('button', { name: 'Supprimer' }).click();
  const dialog = page.getByRole('dialog', { name: 'Supprimer ce fichier ?' });
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Annuler' }).click();
  await expect(dialog).toBeHidden();
  await doomed.getByRole('button', { name: 'Supprimer' }).click();
  await dialog.getByRole('button', { name: 'Supprimer' }).click();
  await expect(doomed).toHaveCount(0);
  await expect(page.getByText(`« ${deletedName} » a été supprimé.`)).toBeVisible();
  expect((await request.get(`/api/files/${ids[2]}`)).status()).toBe(404);

  // The local track and the chosen recording are now in the library.
  expect(((await (await request.get(`/api/files/${ids[0]}`)).json()) as { status: string }).status).toBe('identified');
  await page.goto('/library');
  await page.getByRole('link', { name: `Démos ${stamp}` }).first().click();
  await expect(page.getByRole('heading', { level: 1, name: `Démos ${stamp}` })).toBeVisible();
  await expect(page.getByText('Maquette Douce')).toBeVisible();
});

test('the identify page is usable on a phone', async ({ page, request }) => {
  await page.setViewportSize({ width: 375, height: 800 });
  const dir = mkdtempSync(join(tmpdir(), 'musichub-e2e-identify-'));
  const name = `mobile-${Date.now().toString(36)}.mp3`;
  const id = await upload(request, name, makeMp3(dir, 'm.mp3', 'Petit Écran'));
  await waitAwaiting(request, [id]);
  await page.goto('/identify');
  const card = page.getByRole('article', { name });
  await expect(card.getByRole('button', { name: 'Chercher dans le catalogue' })).toBeVisible();
  await card.getByRole('button', { name: 'Chercher dans le catalogue' }).click();
  await expect(card.getByRole('searchbox')).toHaveValue('Artiste Imaginaire Petit Écran');
  // Nothing overflows the narrow screen.
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  expect(overflow).toBeLessThanOrEqual(0);
  const box = await card.boundingBox();
  expect(box && box.x + box.width).toBeLessThanOrEqual(375);
  // Settings live in the top bar on phones, so the bottom bar keeps five tabs.
  await expect(page.getByRole('navigation').getByRole('link', { name: 'Réglages' })).toBeHidden();
  await page.locator('.mobile-brand').getByRole('link', { name: 'Réglages' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Réglages' })).toBeVisible();
});
