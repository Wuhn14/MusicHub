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

/** Formats a total running time, e.g. "42 min" or "1 h 05 min". */
export function formatTotal(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) {
    return '';
  }
  const minutes = Math.round(ms / 60000);
  if (minutes < 60) {
    return `${minutes} min`;
  }
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
