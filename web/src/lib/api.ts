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
  let res: Response;
  try {
    res = await fetch(url, { signal, headers: { Accept: 'application/json' } });
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

export function coverUrl(albumId: string, size: 250 | 500): string {
  return `/api/covers/${encodeURIComponent(albumId)}?size=${size}`;
}
