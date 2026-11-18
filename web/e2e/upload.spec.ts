import { execFileSync } from 'node:child_process';
import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { expect, test } from '@playwright/test';

// Generates a short invented MP3 (ffmpeg sine wave); no real music is used.
function makeMp3(dir: string, name: string): string {
  const path = join(dir, name);
  execFileSync(
    process.env.MUSICHUB_FFMPEG || 'ffmpeg',
    [
      '-hide_banner', '-loglevel', 'error', '-y',
      '-f', 'lavfi', '-i', 'sine=frequency=330:duration=2',
      '-codec:a', 'libmp3lame', '-b:a', '64k',
      '-metadata', 'title=Chanson Envoyée',
      '-metadata', 'artist=Artiste Inventé',
      path,
    ],
    { stdio: ['ignore', 'ignore', 'inherit'] },
  );
  return path;
}

test('uploads a generated MP3 through the file chooser', async ({ page }) => {
  const dir = mkdtempSync(join(tmpdir(), 'musichub-e2e-upload-'));
  const mp3 = makeMp3(dir, 'envoi-test.mp3');
  const notes = join(dir, 'notes.txt');
  writeFileSync(notes, 'pas de la musique');

  await page.goto('/');
  await page.getByRole('link', { name: 'Ajouter' }).first().click();
  await expect(page).toHaveURL(/\/upload$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Ajouter de la musique' })).toBeVisible();

  const [chooser] = await Promise.all([
    page.waitForEvent('filechooser'),
    page.getByRole('button', { name: 'Choisir des fichiers' }).click(),
  ]);
  await chooser.setFiles([mp3, notes]);

  const sent = page.getByRole('listitem').filter({ hasText: 'envoi-test.mp3' });
  await expect(sent).toBeVisible();
  // Accepted by the server, whatever identification later makes of it.
  await expect(sent.getByText(/Envoyé — identification en cours|Identifié :|À identifier/)).toBeVisible();

  const refused = page.getByRole('listitem').filter({ hasText: 'notes.txt' });
  await expect(refused.getByText('Refusé : format .txt non pris en charge')).toBeVisible();
});
