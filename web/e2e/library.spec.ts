import { expect, test } from '@playwright/test';

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

test('unknown library album key gives a clear error', async ({ page }) => {
  await page.goto('/library/album/bm90LWEta2V5');
  await expect(page.getByRole('alert')).toContainText('Introuvable');
});
