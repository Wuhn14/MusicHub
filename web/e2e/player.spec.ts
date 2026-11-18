import { expect, test, type Page } from '@playwright/test';
import { fixtureAlbum } from './setup';

const [first, second, third] = fixtureAlbum.tracks;
const queueLabel = "File d'attente";

function bar(page: Page) {
  return page.getByRole('contentinfo', { name: 'Lecteur' });
}

/** Displayed position in seconds, read from the seek bar. */
async function position(page: Page): Promise<number> {
  return Number(await bar(page).getByRole('slider', { name: 'Position dans le titre' }).inputValue());
}

async function openAlbum(page: Page): Promise<void> {
  await page.goto('/library');
  await page.getByRole('link', { name: fixtureAlbum.album }).click();
  await expect(page.getByRole('heading', { level: 1, name: fixtureAlbum.album })).toBeVisible();
}

test('plays an album, changes quality, seeks and restores the queue', async ({ page }) => {
  await openAlbum(page);

  // Clicking is the user gesture that allows playback.
  await page.getByRole('button', { name: 'Lire', exact: true }).click();
  await expect(bar(page).getByText(first.title)).toBeVisible();
  await expect(bar(page).getByRole('button', { name: 'Pause' })).toBeVisible();
  // The playing track is highlighted in the list.
  await expect(page.locator('li[aria-current="true"]')).toContainText(first.title);

  // The position advances.
  await expect.poll(() => position(page), { timeout: 10_000 }).toBeGreaterThanOrEqual(1);

  // Switching to MP3 128 restarts the stream as a conversion.
  const converted = page.waitForRequest((r) => r.url().includes('/api/stream/') && r.url().includes('quality=128'));
  await bar(page).getByLabel('Qualité de lecture').selectOption('128');
  await converted;
  const before = await position(page);
  await expect.poll(() => position(page), { timeout: 10_000 }).toBeGreaterThan(before);

  // Seeking in a converted stream reloads it from the new position.
  const seeked = page.waitForRequest((r) => /quality=128&start=2(\.0)?$/.test(r.url()));
  await bar(page).getByRole('slider', { name: 'Position dans le titre' }).fill('2');
  await seeked;
  // Playback goes on after the seek.
  await expect.poll(() => position(page), { timeout: 10_000 }).toBeGreaterThanOrEqual(3);
  await expect(bar(page).getByRole('button', { name: 'Pause' })).toBeVisible();

  // Queue: the rest of the album comes next.
  await bar(page).getByRole('button', { name: queueLabel }).click();
  const panel = page.getByRole('complementary', { name: queueLabel });
  await expect(panel.getByRole('list', { name: 'À suivre' }).getByRole('listitem')).toHaveCount(2);

  // Reload: the queue, the track and the quality survive, without autoplay.
  await page.waitForTimeout(1200); // let the throttled save run
  await page.reload();
  await expect(bar(page).getByText(first.title)).toBeVisible();
  await expect(bar(page).getByRole('button', { name: 'Lecture' })).toBeVisible();
  await expect(bar(page).getByLabel('Qualité de lecture')).toHaveValue('128');
  expect(await position(page)).toBeGreaterThanOrEqual(2);
  await bar(page).getByRole('button', { name: queueLabel }).click();
  const items = page.getByRole('complementary', { name: queueLabel }).getByRole('list').getByRole('listitem');
  await expect(items).toHaveCount(2);
  await expect(items.nth(0)).toContainText(second.title);
  await expect(items.nth(1)).toContainText(third.title);

  // Play resumes where it was.
  await bar(page).getByRole('button', { name: 'Lecture' }).click();
  await expect(bar(page).getByRole('button', { name: 'Pause' })).toBeVisible();
  await expect.poll(() => position(page), { timeout: 10_000 }).toBeGreaterThanOrEqual(4);
});

test('track menu adds to the queue and the queue can be reordered', async ({ page }) => {
  await openAlbum(page);
  await page.getByRole('button', { name: `Lire ${first.title}` }).click();
  await expect(bar(page).getByText(first.title)).toBeVisible();

  await page.getByRole('button', { name: `Plus d'options pour ${first.title}` }).click();
  await page.getByRole('menuitem', { name: 'Ajouter à la file' }).click();
  await expect(page.getByRole('status')).toContainText('ajouté à la file');

  await bar(page).getByRole('button', { name: queueLabel }).click();
  const panel = page.getByRole('complementary', { name: queueLabel });
  const items = panel.getByRole('list', { name: 'À suivre' }).getByRole('listitem');
  await expect(items).toHaveCount(3);
  await expect(items.nth(2)).toContainText(first.title);

  await panel.getByRole('button', { name: `Monter ${third.title}` }).click();
  await expect(items.nth(0)).toContainText(third.title);
  await panel.getByRole('button', { name: `Retirer ${second.title} de la file` }).click();
  await expect(items).toHaveCount(2);

  // Next goes to the new first upcoming track.
  await bar(page).getByRole('button', { name: 'Suivant' }).click();
  await expect(bar(page).getByText(third.title)).toBeVisible();

  await panel.getByRole('button', { name: 'Vider la file' }).click();
  await expect(panel.getByText('Rien à suivre.')).toBeVisible();
});

test('space toggles playback outside of buttons and text fields', async ({ page }) => {
  await openAlbum(page);
  await page.getByRole('button', { name: 'Lire', exact: true }).click();
  await expect(bar(page).getByRole('button', { name: 'Pause' })).toBeVisible();
  await page.locator('h1').click();
  await page.keyboard.press('Space');
  await expect(bar(page).getByRole('button', { name: 'Lecture' })).toBeVisible();
  await page.keyboard.press('Space');
  await expect(bar(page).getByRole('button', { name: 'Pause' })).toBeVisible();
});

test('album cards have a play button', async ({ page }) => {
  await page.goto('/library');
  await page.getByRole('button', { name: `Lire ${fixtureAlbum.album}` }).click();
  await expect(bar(page).getByText(first.title)).toBeVisible();
});
