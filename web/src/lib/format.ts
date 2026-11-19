// Display helpers shared by the pages.

/** Formats a duration as m:ss; returns '' when the length is unknown. */
export function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) {
    return '';
  }
  const total = Math.round(ms / 1000);
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  return `${minutes}:${String(seconds).padStart(2, '0')}`;
}

/** Formats a total running time: "45 s", "1 min 30 s" or "1 h 05 min". */
export function formatTotal(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) {
    return '';
  }
  const seconds = Math.round(ms / 1000);
  if (seconds < 60) {
    return `${seconds} s`;
  }
  if (seconds < 3600) {
    return `${Math.floor(seconds / 60)} min ${String(seconds % 60).padStart(2, '0')} s`.replace(' 00 s', '');
  }
  const minutes = Math.round(seconds / 60);
  return `${Math.floor(minutes / 60)} h ${String(minutes % 60).padStart(2, '0')} min`;
}

/** Returns the year of a MusicBrainz date (YYYY, YYYY-MM or YYYY-MM-DD). */
export function yearOf(date: string): string {
  return /^\d{4}/.test(date) ? date.slice(0, 4) : '';
}

const typeLabels: Record<string, string> = {
  Album: 'Album',
  EP: 'EP',
  Single: 'Single',
  Broadcast: 'Émission',
  Other: 'Autre',
  Compilation: 'Compilation',
  Soundtrack: 'Bande originale',
  Spokenword: 'Texte parlé',
  Interview: 'Interview',
  Audiobook: 'Livre audio',
  'Audio drama': 'Fiction audio',
  Live: 'Live',
  Remix: 'Remix',
  'DJ-mix': 'DJ mix',
  'Mixtape/Street': 'Mixtape',
  Demo: 'Démo',
  'Field recording': 'Enregistrement de terrain',
};

/** Translates a MusicBrainz release group type into French. */
export function typeLabel(type: string): string {
  return typeLabels[type] ?? type;
}

/** Builds the "year · type" line shown under an album title. */
export function albumMeta(date: string, primaryType: string, secondaryTypes: string[]): string {
  const types = secondaryTypes.length > 0 ? secondaryTypes : primaryType ? [primaryType] : [];
  return [yearOf(date), ...types.map(typeLabel)].filter(Boolean).join(' · ');
}

let regionNames: Intl.DisplayNames | null = null;

/** Returns the French name of an ISO 3166 country code, or the code itself. */
export function countryName(code: string): string {
  if (!code) {
    return '';
  }
  try {
    regionNames ??= new Intl.DisplayNames(['fr'], { type: 'region' });
    return regionNames.of(code) ?? code;
  } catch {
    return code;
  }
}

/** Formats a position in seconds as m:ss (0:00 included), for the player. */
export function formatClock(seconds: number): string {
  const total = Number.isFinite(seconds) && seconds > 0 ? Math.floor(seconds) : 0;
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = String(total % 60).padStart(2, '0');
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${s}` : `${m}:${s}`;
}
