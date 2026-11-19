// Typed client for the MusicHub JSON API (see internal/web/api.go).

export interface ArtistHit {
  id: string;
  name: string;
  disambiguation: string;
  country: string;
}

export interface Artist extends ArtistHit {
  sortName: string;
}

export interface AlbumSummary {
  id: string;
  title: string;
  artistId: string;
  artistName: string;
  primaryType: string;
  secondaryTypes: string[];
  firstReleaseDate: string;
}

export interface TrackHit {
  recordingId: string;
  title: string;
  lengthMs: number;
  albumId: string;
  albumTitle: string;
  artistName: string;
}

export interface Track {
  recordingId: string;
  title: string;
  disc: number;
  position: number;
  lengthMs: number;
}

export interface Album extends AlbumSummary {
  tracks: Track[];
}

export interface SearchResult {
  artists: ArtistHit[];
  albums: AlbumSummary[];
  tracks: TrackHit[];
}

export interface ArtistPage {
  artist: Artist;
  albums: AlbumSummary[];
}

/** An API failure carrying a message that can be shown to the user. */
export class ApiError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

/** Reports whether err comes from an aborted request. */
export function isAbort(err: unknown): boolean {
  return err instanceof DOMException && err.name === 'AbortError';
}

/** Returns a French message suitable for display. */
export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    return err.message;
  }
  return 'Une erreur inattendue est survenue.';
}

async function getJSON<T>(url: string, signal?: AbortSignal): Promise<T> {
  return requestJSON<T>(url, { signal });
}

async function requestJSON<T>(url: string, init: RequestInit): Promise<T> {
  const signal = init.signal ?? undefined;
  let res: Response;
  try {
    res = await fetch(url, { ...init, headers: { Accept: 'application/json', ...init.headers } });
  } catch (err) {
    if (isAbort(err)) {
      throw err;
    }
    throw new ApiError('Impossible de joindre le serveur MusicHub.', 0);
  }
  if (!res.ok) {
    let message = `Erreur inattendue du serveur (code ${res.status}).`;
    try {
      const body: unknown = await res.json();
      if (body && typeof body === 'object' && 'error' in body && typeof body.error === 'string') {
        message = body.error;
      }
    } catch (err) {
      if (isAbort(err)) {
        throw err;
      }
    }
    throw new ApiError(message, res.status);
  }
  try {
    return (await res.json()) as T;
  } catch (err) {
    if (isAbort(err)) {
      throw err;
    }
    throw new ApiError('Réponse illisible du serveur.', res.status);
  }
}

export function search(q: string, signal?: AbortSignal): Promise<SearchResult> {
  return getJSON(`/api/search?q=${encodeURIComponent(q)}`, signal);
}

export function getArtist(id: string, signal?: AbortSignal): Promise<ArtistPage> {
  return getJSON(`/api/artists/${encodeURIComponent(id)}`, signal);
}

export function getAlbum(id: string, signal?: AbortSignal): Promise<Album> {
  return getJSON(`/api/albums/${encodeURIComponent(id)}`, signal);
}

/** Recordings of a catalog album that the library can play: recording id → file id. */
export interface AlbumAvailability {
  tracks: Record<string, string>;
}

/** Albums of a catalog artist with something playable: album id → available track count. */
export interface ArtistAvailability {
  albums: Record<string, number>;
}

export function getAlbumAvailability(id: string, signal?: AbortSignal): Promise<AlbumAvailability> {
  return getJSON(`/api/library/availability?album=${encodeURIComponent(id)}`, signal);
}

export function getArtistAvailability(id: string, signal?: AbortSignal): Promise<ArtistAvailability> {
  return getJSON(`/api/library/availability?artist=${encodeURIComponent(id)}`, signal);
}

export function coverUrl(albumId: string, size: 250 | 500): string {
  return `/api/covers/${encodeURIComponent(albumId)}?size=${size}`;
}

/** The user settings as returned by the API; the AcoustID key is masked. */
export interface Settings {
  acoustidKey: string;
  acoustidKeySet: boolean;
  scanIntervalMinutes: number;
  organizeDropped: boolean;
  writeTagsUploaded: boolean;
  writeTagsDropped: boolean;
  duplicates: 'best' | 'keep-both' | 'keep-existing';
  maxUploadBytes: number;
  maxTranscodes: number;
}

/** Every field is optional; an absent field is left unchanged. */
export type SettingsPatch = Partial<Omit<Settings, 'acoustidKeySet'>>;

export interface ToolStatus {
  name: string;
  path: string;
  version: string;
  available: boolean;
  error: string;
}

export function getSettings(signal?: AbortSignal): Promise<Settings> {
  return getJSON('/api/settings', signal);
}

export function saveSettings(patch: SettingsPatch): Promise<Settings> {
  return requestJSON('/api/settings', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(patch),
  });
}

export async function getSystem(signal?: AbortSignal): Promise<ToolStatus[]> {
  const res = await getJSON<{ tools: ToolStatus[] }>('/api/system', signal);
  return res.tools;
}

// Local library.

export type LibrarySort = 'added' | 'name' | 'year';

export interface LibAlbum {
  key: string;
  title: string;
  artist: string;
  year: string;
  coverUrl: string;
  /** MusicBrainz release group id; empty when the album is not identified. */
  albumId: string;
  trackCount: number;
  durationMs: number;
  local: boolean;
}

export interface LibArtist {
  name: string;
  albumCount: number;
  trackCount: number;
}

export interface LibTrack {
  fileId: string;
  title: string;
  artist: string;
  album: string;
  albumKey: string;
  disc: number;
  position: number;
  durationMs: number;
  recordingId: string;
  local: boolean;
  status: string;
  coverUrl: string;
}

export interface LibAlbumPage extends LibAlbum {
  tracks: LibTrack[];
}

export interface ScanReport {
  seen: number;
  added: number;
  updated: number;
  unreadable: number;
  missing: number;
  errors: number;
  /** Folders and files the scan could not read (permissions). */
  skipped: number;
}

export interface ScanStatus {
  running: boolean;
  lastRun: string;
  lastError: string;
  lastReport: ScanReport;
}

export async function getLibraryAlbums(sort: LibrarySort, signal?: AbortSignal): Promise<LibAlbum[]> {
  return (await getJSON<{ albums: LibAlbum[] }>(`/api/library/albums?sort=${sort}`, signal)).albums;
}

export async function getLibraryArtists(signal?: AbortSignal): Promise<LibArtist[]> {
  return (await getJSON<{ artists: LibArtist[] }>('/api/library/artists', signal)).artists;
}

export async function getLibraryTracks(sort: LibrarySort, q: string, signal?: AbortSignal): Promise<LibTrack[]> {
  const url = `/api/library/tracks?sort=${sort}&q=${encodeURIComponent(q)}`;
  return (await getJSON<{ tracks: LibTrack[] }>(url, signal)).tracks;
}

export function getLibraryAlbum(key: string, signal?: AbortSignal): Promise<LibAlbumPage> {
  return getJSON(`/api/library/albums/${encodeURIComponent(key)}`, signal);
}

export function getScanStatus(signal?: AbortSignal): Promise<ScanStatus> {
  return getJSON('/api/library/status', signal);
}

export async function startScan(): Promise<void> {
  await requestJSON<unknown>('/api/library/scan', { method: 'POST' });
}

// "À identifier".

export interface IdentifyCandidate {
  recordingId: string;
  albumId: string;
  title: string;
  artist: string;
  album: string;
  lengthMs: number;
  /** 0 to 100. */
  score: number;
  /** 'tag', 'acoustid' or 'search'. */
  source: string;
}

export interface IdentifyTags {
  title: string;
  artist: string;
  album: string;
  albumArtist: string;
  year: string;
  track: number;
  disc: number;
}

export interface IdentifyFile {
  id: string;
  /** File name only (the sender's name for an upload). */
  name: string;
  origin: 'uploaded' | 'dropped';
  format: string;
  durationMs: number;
  tags: IdentifyTags;
  candidates: IdentifyCandidate[];
}

export interface IdentifyList {
  files: IdentifyFile[];
  /** Files waiting for the user, in all (files holds at most 100). */
  total: number;
  /** Files still being identified automatically. */
  queued: number;
}

export interface LocalTrackInput {
  title: string;
  artist: string;
  album: string;
  albumArtist: string;
  disc: number;
  position: number;
  year: number;
}

export function getIdentifyList(signal?: AbortSignal): Promise<IdentifyList> {
  return getJSON('/api/identify', signal);
}

export function getIdentifyCount(signal?: AbortSignal): Promise<{ count: number; queued: number }> {
  return getJSON('/api/identify/count', signal);
}

function postJSON<T>(url: string, body: unknown): Promise<T> {
  return requestJSON<T>(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

export function chooseRecording(fileId: string, recordingId: string, albumId = ''): Promise<{ status: string; duplicate: string }> {
  const body: { recordingId: string; albumId?: string } = { recordingId };
  if (albumId) {
    body.albumId = albumId;
  }
  return postJSON(`/api/identify/${encodeURIComponent(fileId)}/choose`, body);
}

export function createLocalTrack(fileId: string, input: LocalTrackInput): Promise<{ localTrackId: string; status: string }> {
  return postJSON(`/api/identify/${encodeURIComponent(fileId)}/local`, input);
}

export function uploadLocalCover(fileId: string, image: Blob): Promise<{ coverUrl: string }> {
  return requestJSON(`/api/identify/${encodeURIComponent(fileId)}/cover`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/octet-stream' },
    body: image,
  });
}

export async function deletePendingFile(fileId: string): Promise<void> {
  await requestJSON<unknown>(`/api/identify/${encodeURIComponent(fileId)}`, { method: 'DELETE' });
}
