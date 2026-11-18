import { expect, test } from '@playwright/test';
import { emptyAppPort, fixtureAlbum } from './setup';

test.describe('with an empty music folder', () => {
  test.use({ baseURL: `http://127.0.0.1:${emptyAppPort}` });

  test('library page shows tabs, an empty state and the scan button', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('link', { name: 'Bibliothèque' }).first().click();
    await expect(page).toHaveURL(/\/library$/);
    await expect(page.getByRole('heading', { level: 1, name: 'Bibliothèque' })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Ta bibliothèque est vide' })).toBeVisible();

    // The tab lives in the query string, so back and forward keep working.
    await page.getByRole('link', { name: 'Titres' }).click();
    await expect(page).toHaveURL(/tab=tracks/);
    await expect(page.getByPlaceholder('Filtrer les titres…')).toBeVisible();
    await page.getByRole('link', { name: 'Artistes' }).click();
    await expect(page).toHaveURL(/tab=artists/);
    await page.goBack();
    await expect(page).toHaveURL(/tab=tracks/);

    await page.getByRole('button', { name: 'Analyser maintenant' }).click();
    // The button must come back to its idle, enabled state (no stuck spinner).
    await expect(page.getByRole('button', { name: 'Analyser maintenant' })).toBeEnabled({ timeout: 15_000 });
    await expect(page.getByText(/Dernière analyse : \d+ fichiers?/)).toBeVisible();
  });
});

test('library lists the scanned album and its tracks', async ({ page }) => {
  await page.goto('/library');
  await page.getByRole('link', { name: fixtureAlbum.album }).click();
  await expect(page.getByRole('heading', { level: 1, name: fixtureAlbum.album })).toBeVisible();
  for (const t of fixtureAlbum.tracks) {
    await expect(page.getByRole('button', { name: `Lire ${t.title}` })).toBeVisible();
  }
});

test('unknown library album key gives a clear error', async ({ page }) => {
  await page.goto('/library/album/bm90LWEta2V5');
  await expect(page.getByRole('alert')).toContainText('Introuvable');
});
