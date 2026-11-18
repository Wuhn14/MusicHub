import { expect, test } from '@playwright/test';

// All data comes from the invented fixtures served by cmd/fakemb.
test('search an artist, open its page, then an album with its tracks', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('link', { name: 'Recherche' }).first().click();

  await page.getByLabel('Rechercher un artiste, un album ou un titre').fill('Lumen Harbor');

  const artistLink = page.getByRole('link', { name: /Lumen Harbor/ }).first();
  await artistLink.click();
  await expect(page).toHaveURL(/\/artist\//);
  await expect(page.getByRole('heading', { level: 1, name: 'Lumen Harbor' })).toBeVisible();

  // The invented artist has no discography: go back to the search results
  // and open the album from there.
  await page.goBack();
  await page.getByRole('link', { name: /Paper Lanterns/ }).first().click();
  await expect(page).toHaveURL(/\/album\//);
  await expect(page.getByRole('heading', { level: 1, name: 'Paper Lanterns' })).toBeVisible();

  const tracks = page.getByRole('listitem').filter({ hasText: /Opening Tide|Second Wind|Silent Close/ });
  await expect(tracks).toHaveCount(3);
  await expect(tracks.filter({ hasText: 'Opening Tide' })).toContainText('3:00');
  await expect(tracks.filter({ hasText: 'Second Wind' })).toContainText('4:00');
  // Silent Close has no length in the fixture: no duration is shown.
  await expect(tracks.filter({ hasText: 'Silent Close' })).not.toContainText(/\d:\d\d/);
});
