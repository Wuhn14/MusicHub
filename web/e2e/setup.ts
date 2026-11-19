// Fills the test music folder with generated sounds before the app starts.
// No real music is ever used: ffmpeg synthesizes sine waves and the tags are
// invented.

import { execFileSync } from 'node:child_process';
import { join } from 'node:path';

/** Port of a second app instance whose music folder stays empty. */
export const emptyAppPort = 18091;

export const fixtureAlbum = {
  artist: 'Les Ondes Test',
  album: 'Sinus Variations',
  year: '2024',
  tracks: [
    { title: 'Premier Signal', seconds: 10, frequency: 440 },
    { title: 'Deuxième Signal', seconds: 9, frequency: 523 },
    { title: 'Troisième Signal', seconds: 11, frequency: 659 },
  ],
};

/** Writes the fixture album as MP3 files into dir (synchronously). */
export function generateMusic(dir: string): void {
  const ffmpeg = process.env.MUSICHUB_FFMPEG || 'ffmpeg';
  const { artist, album, year, tracks } = fixtureAlbum;
  tracks.forEach((t, i) => {
    const n = i + 1;
    execFileSync(
      ffmpeg,
      [
        '-hide_banner',
        '-loglevel', 'error',
        '-y',
        '-f', 'lavfi',
        '-i', `sine=frequency=${t.frequency}:duration=${t.seconds}`,
        '-ac', '2',
        '-codec:a', 'libmp3lame',
        '-b:a', '128k',
        '-metadata', `artist=${artist}`,
        '-metadata', `album_artist=${artist}`,
        '-metadata', `album=${album}`,
        '-metadata', `title=${t.title}`,
        '-metadata', `track=${n}/${tracks.length}`,
        '-metadata', `date=${year}`,
        join(dir, `${String(n).padStart(2, '0')} - ${t.title}.mp3`),
      ],
      { stdio: ['ignore', 'ignore', 'inherit'] },
    );
  });
}
