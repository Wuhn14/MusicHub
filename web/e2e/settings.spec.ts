import { expect, test } from '@playwright/test';

test('settings page saves values and masks the AcoustID key', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('link', { name: 'Réglages' }).first().click();
  await expect(page).toHaveURL(/\/settings$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Réglages' })).toBeVisible();

  await page.getByLabel('Intervalle d’analyse (minutes)').fill('30');
  await page.getByLabel('Doublons').selectOption('keep-both');
  await page.getByLabel('Clé AcoustID').fill('invented-key-wxyz');
  await page.getByRole('button', { name: 'Enregistrer' }).click();
  await expect(page.getByText('Réglages enregistrés.')).toBeVisible();

  // After a reload the key is only shown masked, never in full.
  await page.reload();
  await expect(page.getByLabel('Clé AcoustID')).toHaveAttribute('placeholder', '••••wxyz');
  await expect(page.getByLabel('Intervalle d’analyse (minutes)')).toHaveValue('30');
  await expect(page.getByLabel('Doublons')).toHaveValue('keep-both');

  await page.getByRole('button', { name: 'Supprimer la clé' }).click();
  await expect(page.getByLabel('Clé AcoustID')).toHaveAttribute('placeholder', 'Aucune clé enregistrée');

  // Tool status block lists the three tools.
  await expect(page.getByText('ffprobe', { exact: true })).toBeVisible();
});
