// Browser side of the upload: extension filter, drag-and-drop folder
// walking, one XMLHttpRequest per file (fetch cannot report upload progress)
// and the status poll that follows.

export const allowedExtensions = ['.mp3', '.flac', '.m4a', '.aac', '.ogg', '.oga', '.opus', '.wav', '.aif', '.aiff'];

/** How many files are sent at the same time. */
export const maxConcurrentUploads = 3;

/** The file input `accept` value: the extensions, plus audio/* for phone pickers. */
export const acceptAttribute = [...allowedExtensions, 'audio/*'].join(',');

/** Lowercased extension of a file name, with its dot, or ''. */
export function extensionOf(name: string): string {
  const base = name.slice(Math.max(name.lastIndexOf('/'), name.lastIndexOf('\\')) + 1);
  const dot = base.lastIndexOf('.');
  return dot > 0 ? base.slice(dot).toLowerCase() : '';
}

export function isAllowedName(name: string): boolean {
  return allowedExtensions.includes(extensionOf(name));
}

/** A French refusal reason, lower-cased and without final period, for "Refusé : …". */
export function reasonFromMessage(message: string): string {
  const m = message.trim().replace(/[.]$/, '');
  return m.charAt(0).toLowerCase() + m.slice(1);
}

export interface SentFile {
  id: string;
  name: string;
  status: string;
  durationMs: number;
  format: string;
}

/** An upload failure; message is French, status the HTTP status (0 when the connection failed). */
export class UploadError extends Error {
  readonly status: number;
  readonly aborted: boolean;

  constructor(message: string, status: number, aborted = false) {
    super(message);
    this.name = 'UploadError';
    this.status = status;
    this.aborted = aborted;
  }
}

export interface UploadHandle {
  promise: Promise<SentFile>;
  abort(): void;
}

/** Sends one file. onProgress receives a fraction between 0 and 1. */
export function uploadFile(file: File, onProgress: (fraction: number) => void): UploadHandle {
  const xhr = new XMLHttpRequest();
  const promise = new Promise<SentFile>((resolve, reject) => {
    xhr.open('POST', '/api/uploads');
    xhr.setRequestHeader('X-File-Name', encodeURIComponent(file.name));
    xhr.setRequestHeader('Content-Type', 'application/octet-stream');
    xhr.setRequestHeader('Accept', 'application/json');
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && e.total > 0) {
        onProgress(e.loaded / e.total);
      }
    };
    xhr.onload = () => {
      let body: unknown = null;
      try {
        body = JSON.parse(xhr.responseText);
      } catch {
        // Not JSON: handled below.
      }
      if (xhr.status === 201 && body && typeof body === 'object') {
        resolve(body as SentFile);
        return;
      }
      const msg =
        body && typeof body === 'object' && 'error' in body && typeof body.error === 'string'
          ? body.error
          : `Erreur inattendue du serveur (code ${xhr.status}).`;
      reject(new UploadError(msg, xhr.status));
    };
    xhr.onerror = () => reject(new UploadError('Connexion interrompue.', 0));
    xhr.onabort = () => reject(new UploadError('Envoi annulé.', 0, true));
    xhr.send(file);
  });
  return { promise, abort: () => xhr.abort() };
}

export interface FileStatus {
  id: string;
  status: string;
  title: string;
  artist: string;
  album: string;
}

export async function getFileStatus(id: string): Promise<FileStatus> {
  const res = await fetch(`/api/files/${encodeURIComponent(id)}`, { headers: { Accept: 'application/json' } });
  if (!res.ok) {
    throw new Error(`status ${res.status}`);
  }
  return (await res.json()) as FileStatus;
}

// Minimal typings for the non-standard directory API.
interface FsEntry {
  isFile: boolean;
  isDirectory: boolean;
  name: string;
}
interface FsFileEntry extends FsEntry {
  file(ok: (f: File) => void, ko: (e: unknown) => void): void;
}
interface FsDirEntry extends FsEntry {
  createReader(): { readEntries(ok: (e: FsEntry[]) => void, ko: (e: unknown) => void): void };
}

function readAll(dir: FsDirEntry): Promise<FsEntry[]> {
  const reader = dir.createReader();
  return new Promise((resolve, reject) => {
    const all: FsEntry[] = [];
    const next = () =>
      reader.readEntries((batch) => {
        if (batch.length === 0) {
          resolve(all);
        } else {
          all.push(...batch);
          next(); // readEntries returns at most a batch at a time
        }
      }, reject);
    next();
  });
}

async function walk(entry: FsEntry, out: File[]): Promise<void> {
  if (entry.name.startsWith('.')) {
    return;
  }
  if (entry.isFile) {
    out.push(await new Promise<File>((ok, ko) => (entry as FsFileEntry).file(ok, ko)));
  } else if (entry.isDirectory) {
    for (const child of await readAll(entry as FsDirEntry)) {
      await walk(child, out);
    }
  }
}

/** Every file of a drop event, descending into dropped folders. */
export async function filesFromDrop(dt: DataTransfer): Promise<File[]> {
  // The entries must all be taken now: the items die when the handler yields.
  const entries: FsEntry[] = [];
  const loose: File[] = [];
  for (const item of Array.from(dt.items)) {
    if (item.kind !== 'file') {
      continue;
    }
    const entry = (item as DataTransferItem & { webkitGetAsEntry?: () => FsEntry | null }).webkitGetAsEntry?.();
    if (entry) {
      entries.push(entry);
    } else {
      const f = item.getAsFile();
      if (f) {
        loose.push(f);
      }
    }
  }
  const out: File[] = [...loose];
  for (const e of entries) {
    try {
      await walk(e, out);
    } catch {
      // An unreadable entry is skipped; the others still go through.
    }
  }
  return out;
}
