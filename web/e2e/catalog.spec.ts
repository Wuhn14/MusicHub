import { expect, test } from '@playwright/test';

// All data comes from the invented fixtures served by cmd/fakemb.
test('search an artist, open its Albums section, then an album with its tracks', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('link', { name: 'Recherche' }).first().click();

  await page.getByLabel('Rechercher un artiste, un album ou un titre').fill('Lumen Harbor');

  const artistLink = page.getByRole('link', { name: /Lumen Harbor/ }).first();
  await artistLink.click();
  await expect(page).toHaveURL(/\/artist\//);
  await expect(page.getByRole('heading', { level: 1, name: 'Lumen Harbor' })).toBeVisible();

  // The discography lists the album under "Albums"; open it from there.
  const albums = page.getByRole('region', { name: 'Albums' });
  await expect(albums).toBeVisible();
  await albums.getByRole('link', { name: /Paper Lanterns/ }).click();
  await expect(page).toHaveURL(/\/album\//);
  await expect(page.getByRole('heading', { level: 1, name: 'Paper Lanterns' })).toBeVisible();

  const tracks = page.getByRole('listitem').filter({ hasText: /Opening Tide|Second Wind|Silent Close/ });
  await expect(tracks).toHaveCount(3);
  await expect(tracks.filter({ hasText: 'Opening Tide' })).toContainText('3:00');
  await expect(tracks.filter({ hasText: 'Second Wind' })).toContainText('4:00');
  // Silent Close has no length in the fixture: no duration is shown.
  await expect(tracks.filter({ hasText: 'Silent Close' })).not.toContainText(/\d:\d\d/);
});

// The owned tracks come from a mocked availability answer pointing at a
// generated library file, so the catalog page plays real audio.
test('a catalog album plays the tracks of the library and greys the others', async ({ page, request }) => {
  const { tracks } = (await (await request.get('/api/library/tracks')).json()) as { tracks: { fileId: string }[] };
  expect(tracks.length).toBeGreaterThan(0);
  const fileId = tracks[0].fileId;
  await page.route('**/api/library/availability?album=*', (route) =>
    route.fulfill({ json: { tracks: { '00000000-0000-4000-8000-0000000000e2': fileId } } }),
  );
  await page.route('**/api/library/availability?artist=*', (route) =>
    route.fulfill({ json: { albums: { '00000000-0000-4000-8000-0000000000b1': 1 } } }),
  );

  // The artist page marks the album as owned.
  await page.goto('/artist/00000000-0000-4000-8000-0000000000a1');
  const card = page.getByRole('main').getByRole('link', { name: /Paper Lanterns/ });
  await expect(card).toContainText('Dans ta bibliothèque');

  await card.click();
  await expect(page.getByRole('heading', { level: 1, name: 'Paper Lanterns' })).toBeVisible();
  await expect(page.getByText('1 sur 3 dans ta bibliothèque')).toBeVisible();

  // Missing tracks are greyed and cannot be played.
  const missing = page.getByRole('group').filter({ hasText: 'Opening Tide' });
  await expect(missing).toHaveAttribute('aria-disabled', 'true');
  await expect(missing).toHaveAttribute('title', 'Pas encore dans ta bibliothèque');
  await expect(page.getByRole('button', { name: 'Lire Opening Tide' })).toHaveCount(0);

  await page.getByRole('button', { name: 'Lire Second Wind' }).click();
  const bar = page.getByRole('contentinfo', { name: 'Lecteur' });
  await expect(bar.getByText('Second Wind')).toBeVisible();
  await expect(bar.getByRole('button', { name: 'Pause' })).toBeVisible();
  await expect(page.locator('li[aria-current="true"]')).toContainText('Second Wind');
});
