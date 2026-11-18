// Groups an artist's release groups the way a streaming app shows them:
// studio releases first, then compilations/live/remixes, then the rest.

import type { AlbumSummary } from './api';

export interface DiscographyGroup {
  label: string;
  albums: AlbumSummary[];
}

/** Newest first; release groups without a date go last. */
export function byDateDesc(a: AlbumSummary, b: AlbumSummary): number {
  if (!a.firstReleaseDate || !b.firstReleaseDate) {
    return a.firstReleaseDate ? -1 : b.firstReleaseDate ? 1 : 0;
  }
  return b.firstReleaseDate.localeCompare(a.firstReleaseDate);
}

const studioGroups: Record<string, string> = {
  Album: 'Albums',
  EP: 'EP',
  Single: 'Singles',
};

const groupOrder = ['Albums', 'EP', 'Singles', 'Compilations, live et remix', 'Autres'];

function groupOf(album: AlbumSummary): string {
  if (album.secondaryTypes.length > 0) {
    return 'Compilations, live et remix';
  }
  return studioGroups[album.primaryType] ?? 'Autres';
}

/** Returns the non-empty groups in display order, each sorted by date. */
export function groupDiscography(albums: AlbumSummary[]): DiscographyGroup[] {
  const groups = new Map<string, AlbumSummary[]>(groupOrder.map((label) => [label, []]));
  for (const album of albums) {
    groups.get(groupOf(album))?.push(album);
  }
  return groupOrder
    .map((label) => ({ label, albums: [...(groups.get(label) ?? [])].sort(byDateDesc) }))
    .filter((g) => g.albums.length > 0);
}
