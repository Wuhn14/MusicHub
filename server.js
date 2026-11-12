// MusicHub : une seule interface pour chercher, demander (Lidarr + Prowlarr) et écouter (Navidrome).
// Aucune dépendance : Node 20+ suffit.
import http from 'node:http';
import crypto from 'node:crypto';
import fs from 'node:fs/promises';
import path from 'node:path';
import { Readable } from 'node:stream';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const PUBLIC = path.join(__dirname, 'public');

const env = (k, d = '') => (process.env[k] ?? d).trim();
const strip = (u) => u.replace(/\/+$/, '');
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const httpError = (status, message) => Object.assign(new Error(message), { status });

const cfg = {
  port: Number(env('PORT', '8080')),
  lidarr: { name: 'Lidarr', url: strip(env('LIDARR_URL')), key: env('LIDARR_API_KEY') },
  prowlarr: { name: 'Prowlarr', url: strip(env('PROWLARR_URL')), key: env('PROWLARR_API_KEY') },
  navidrome: {
    url: strip(env('NAVIDROME_URL')),
    user: env('NAVIDROME_USER'),
    pass: env('NAVIDROME_PASSWORD'),
    transcode: env('NAVIDROME_TRANSCODE'), // ex. "mp3" pour forcer une conversion en 320 kb/s
  },
  rootFolder: env('LIDARR_ROOT_FOLDER'),
  qualityProfileId: Number(env('LIDARR_QUALITY_PROFILE_ID')) || null,
  metadataProfileId: Number(env('LIDARR_METADATA_PROFILE_ID')) || null,
  autoScan: env('AUTO_SCAN', 'true') !== 'false',
};

/* ---------- Lidarr / Prowlarr (API v1, clé dans X-Api-Key) ---------- */

async function arr(svc, p, { method = 'GET', body } = {}) {
  if (!svc.url || !svc.key) throw httpError(503, `${svc.name} n'est pas configuré`);
  let r;
  try {
    r = await fetch(`${svc.url}/api/v1${p}`, {
      method,
      headers: { 'X-Api-Key': svc.key, ...(body ? { 'Content-Type': 'application/json' } : {}) },
      body: body ? JSON.stringify(body) : undefined,
      signal: AbortSignal.timeout(30000),
    });
  } catch (e) {
    throw httpError(502, `${svc.name} est injoignable (${e.cause?.code || e.message})`);
  }
  if (!r.ok) {
    const t = await r.text().catch(() => '');
    throw httpError(502, `${svc.name} a répondu ${r.status} ${t.slice(0, 200)}`.trim());
  }
  const txt = await r.text();
  return txt ? JSON.parse(txt) : null;
}
const lidarr = (p, o) => arr(cfg.lidarr, p, o);
const prowlarr = (p, o) => arr(cfg.prowlarr, p, o);

/* ---------- Navidrome (API Subsonic) ---------- */

function ndParams(extra = {}) {
  const { user, pass } = cfg.navidrome;
  const salt = crypto.randomBytes(8).toString('hex');
  const token = crypto.createHash('md5').update(pass + salt).digest('hex');
  return new URLSearchParams({ u: user, t: token, s: salt, v: '1.16.1', c: 'musichub', f: 'json', ...extra });
}

function ndReady() {
  if (!cfg.navidrome.url || !cfg.navidrome.user || !cfg.navidrome.pass) {
    throw httpError(503, "Navidrome n'est pas configuré");
  }
}

async function nd(endpoint, extra = {}) {
  ndReady();
  let r;
  try {
    r = await fetch(`${cfg.navidrome.url}/rest/${endpoint}?${ndParams(extra)}`, { signal: AbortSignal.timeout(30000) });
  } catch (e) {
    throw httpError(502, `Navidrome est injoignable (${e.cause?.code || e.message})`);
  }
  if (!r.ok) throw httpError(502, `Navidrome a répondu ${r.status}`);
  const resp = (await r.json())['subsonic-response'];
  if (resp?.status !== 'ok') throw httpError(502, resp?.error?.message || 'Erreur Navidrome');
  return resp;
}

async function proxyBinary(req, res, endpoint, params) {
  ndReady();
  const ac = new AbortController();
  res.on('close', () => ac.abort());
  const headers = {};
  if (req.headers.range) headers.Range = req.headers.range;
  const r = await fetch(`${cfg.navidrome.url}/rest/${endpoint}?${ndParams(params)}`, { headers, signal: ac.signal });
  const out = {};
  for (const h of ['content-type', 'content-length', 'content-range', 'accept-ranges', 'etag', 'last-modified']) {
    const v = r.headers.get(h);
    if (v) out[h] = v;
  }
  if (endpoint === 'getCoverArt') out['cache-control'] = 'private, max-age=86400';
  res.writeHead(r.status, out);
  if (!r.body) return res.end();
  Readable.fromWeb(r.body).on('error', () => res.destroy()).pipe(res);
}

const mapAlbum = (a) => ({
  id: a.id,
  name: a.name || a.title,
  artist: a.artist,
  year: a.year || null,
  coverArt: a.coverArt || a.id,
  songCount: a.songCount || 0,
  duration: a.duration || 0,
});
const mapSong = (s) => ({
  id: s.id,
  title: s.title,
  track: s.track || 0,
  disc: s.discNumber || 1,
  duration: s.duration || 0,
  artist: s.artist,
  album: s.album,
  albumId: s.albumId,
  coverArt: s.coverArt || s.albumId,
});

/* ---------- Recherche et demande (Lidarr) ---------- */

const lookupCache = new Map(); // foreignAlbumId -> résultat de recherche Lidarr
function remember(a) {
  lookupCache.set(a.foreignAlbumId, a);
  if (lookupCache.size > 500) lookupCache.delete(lookupCache.keys().next().value);
}

function mapLookupAlbum(a) {
  const cover = (a.images || []).find((i) => i.coverType === 'cover')?.remoteUrl || a.images?.[0]?.remoteUrl || null;
  const stats = a.statistics || {};
  return {
    foreignAlbumId: a.foreignAlbumId,
    title: a.title,
    artist: a.artist?.artistName || '',
    year: a.releaseDate ? a.releaseDate.slice(0, 4) : null,
    type: a.albumType || '',
    cover,
    libraryId: a.id > 0 ? a.id : null,
    complete: (stats.totalTrackCount || 0) > 0 && (stats.trackFileCount || 0) >= stats.totalTrackCount,
    monitored: !!a.monitored,
  };
}

async function searchRemote(q) {
  const results = await lidarr(`/album/lookup?term=${encodeURIComponent(q)}`);
  const albums = (results || []).slice(0, 36);
  albums.forEach(remember);
  return albums.map(mapLookupAlbum);
}

// Une demande à la fois : évite d'ajouter deux fois le même artiste dans Lidarr.
let lock = Promise.resolve();
function serialized(fn) {
  const run = lock.then(() => fn());
  lock = run.catch(() => {});
  return run;
}

async function requestAlbum(foreignAlbumId) {
  let album = lookupCache.get(foreignAlbumId);
  if (!album) {
    const r = await lidarr(`/album/lookup?term=${encodeURIComponent('lidarr:' + foreignAlbumId)}`);
    album = (r || []).find((x) => x.foreignAlbumId === foreignAlbumId);
    if (!album) throw httpError(404, 'Album introuvable');
  }

  const foreignArtistId = album.artist.foreignArtistId;
  const artists = await lidarr('/artist');
  let artist = artists.find((a) => a.foreignArtistId === foreignArtistId);

  if (!artist) {
    const [roots, quality, meta] = await Promise.all([
      lidarr('/rootfolder'),
      lidarr('/qualityprofile'),
      lidarr('/metadataprofile'),
    ]);
    const rootFolderPath = cfg.rootFolder || roots[0]?.path;
    if (!rootFolderPath) throw httpError(400, "Aucun dossier racine n'est défini dans Lidarr");
    artist = await lidarr('/artist', {
      method: 'POST',
      body: {
        ...album.artist,
        qualityProfileId: cfg.qualityProfileId || quality[0]?.id,
        metadataProfileId: cfg.metadataProfileId || meta[0]?.id,
        rootFolderPath,
        monitored: true,
        monitorNewItems: 'none', // on ne télécharge que l'album demandé
        addOptions: { monitor: 'none', searchForMissingAlbums: false },
      },
    });
  }

  // Lidarr charge les albums de l'artiste en arrière-plan juste après l'ajout.
  let found = null;
  for (let i = 0; i < 40 && !found; i++) {
    const albums = await lidarr(`/album?artistId=${artist.id}`);
    found = albums.find((a) => a.foreignAlbumId === foreignAlbumId);
    if (!found) await sleep(1500);
  }
  if (!found) throw httpError(504, "Lidarr n'a pas encore chargé les albums de cet artiste. Réessaie dans une minute.");

  const s = found.statistics || {};
  if ((s.totalTrackCount || 0) > 0 && (s.trackFileCount || 0) >= s.totalTrackCount) {
    return { already: true, albumId: found.id };
  }
  await lidarr('/album/monitor', { method: 'PUT', body: { albumIds: [found.id], monitored: true } });
  await lidarr('/command', { method: 'POST', body: { name: 'AlbumSearch', albumIds: [found.id] } });
  return { ok: true, albumId: found.id };
}

async function getQueue() {
  const [queue, missing] = await Promise.all([
    lidarr('/queue?page=1&pageSize=50&includeArtist=true&includeAlbum=true'),
    lidarr('/wanted/missing?page=1&pageSize=30&includeArtist=true&monitored=true').catch(() => ({ records: [] })),
  ]);
  return {
    downloading: (queue.records || []).map((x) => ({
      id: x.id,
      albumId: x.albumId,
      artist: x.artist?.artistName || '',
      album: x.album?.title || x.title,
      state: x.trackedDownloadState || x.status,
      progress: x.size > 0 ? Math.round((100 * (x.size - x.sizeleft)) / x.size) : 0,
      timeleft: x.timeleft || null,
      problem: x.errorMessage || (x.statusMessages || []).flatMap((m) => m.messages || [])[0] || null,
    })),
    waiting: (missing.records || []).map((x) => ({
      albumId: x.id,
      artist: x.artist?.artistName || '',
      album: x.title,
      year: x.releaseDate ? x.releaseDate.slice(0, 4) : null,
    })),
  };
}

/* ---------- État des services et indexeurs (Prowlarr) ---------- */

async function getStatus() {
  const check = async (fn) => {
    try {
      return { ok: true, ...(await fn()) };
    } catch (e) {
      return { ok: false, error: e.message };
    }
  };
  const [l, p, n] = await Promise.all([
    check(async () => ({ version: (await lidarr('/system/status')).version })),
    check(async () => ({ version: (await prowlarr('/system/status')).version })),
    check(async () => {
      const r = await nd('ping');
      return { version: r.serverVersion || r.version };
    }),
  ]);
  return { lidarr: l, prowlarr: p, navidrome: n };
}

async function getIndexers() {
  const [indexers, statuses, health] = await Promise.all([
    prowlarr('/indexer'),
    prowlarr('/indexerstatus').catch(() => []),
    prowlarr('/health').catch(() => []),
  ]);
  const now = Date.now();
  const failing = new Set(
    statuses.filter((s) => s.disabledTill && new Date(s.disabledTill).getTime() > now).map((s) => s.indexerId),
  );
  return {
    indexers: indexers.map((i) => ({
      id: i.id,
      name: i.name,
      enable: !!i.enable,
      protocol: i.protocol,
      privacy: i.privacy,
      failing: failing.has(i.id),
    })),
    health: health.map((h) => ({ type: h.type, message: h.message })),
  };
}

async function prowlarrSearch(q) {
  const r = await prowlarr(`/search?query=${encodeURIComponent(q)}&type=search&categories=3000&limit=25`);
  return (r || []).slice(0, 25).map((x) => ({
    title: x.title,
    indexer: x.indexer,
    protocol: x.protocol,
    size: x.size || 0,
    seeders: x.seeders ?? null,
  }));
}

/* ---------- Scan automatique de Navidrome ---------- */

if (cfg.autoScan && cfg.lidarr.url && cfg.navidrome.url) {
  let hadQueue = false;
  setInterval(async () => {
    try {
      const q = await lidarr('/queue?page=1&pageSize=1');
      const n = q?.totalRecords || 0;
      if (hadQueue && n === 0) {
        await sleep(10000); // laisse à Lidarr le temps de finir l'import
        await nd('startScan');
      }
      hadQueue = n > 0;
    } catch {
      /* service momentanément indisponible */
    }
  }, 30000).unref();
}

/* ---------- Serveur HTTP ---------- */

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.json': 'application/json',
  '.png': 'image/png',
};

function sendJson(res, status, data) {
  const body = JSON.stringify(data);
  res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8', 'Cache-Control': 'no-store' });
  res.end(body);
}

async function readJson(req) {
  let size = 0;
  const chunks = [];
  for await (const c of req) {
    size += c.length;
    if (size > 100_000) throw httpError(413, 'Requête trop volumineuse');
    chunks.push(c);
  }
  try {
    return chunks.length ? JSON.parse(Buffer.concat(chunks).toString()) : {};
  } catch {
    throw httpError(400, 'JSON invalide');
  }
}

async function api(req, res, url, p) {
  const q = url.searchParams;
  const m = req.method;

  if (m === 'GET' && p === '/api/status') return sendJson(res, 200, await getStatus());

  // Recherche dans la bibliothèque (Navidrome)
  if (m === 'GET' && p === '/api/library/search') {
    const r = await nd('search3', { query: q.get('q') || '', artistCount: 0, albumCount: 24, songCount: 30 });
    const s = r.searchResult3 || {};
    return sendJson(res, 200, { albums: (s.album || []).map(mapAlbum), songs: (s.song || []).map(mapSong) });
  }
  if (m === 'GET' && p === '/api/library/albums') {
    const type = ['newest', 'alphabeticalByName', 'random', 'recent', 'frequent'].includes(q.get('type'))
      ? q.get('type')
      : 'newest';
    const r = await nd('getAlbumList2', { type, size: Math.min(Number(q.get('size')) || 60, 200), offset: Number(q.get('offset')) || 0 });
    return sendJson(res, 200, { albums: (r.albumList2?.album || []).map(mapAlbum) });
  }
  let mm = p.match(/^\/api\/library\/album\/([\w-]+)$/);
  if (m === 'GET' && mm) {
    const r = await nd('getAlbum', { id: mm[1] });
    const a = r.album;
    return sendJson(res, 200, { ...mapAlbum(a), songs: (a.song || []).map(mapSong) });
  }
  if (m === 'POST' && p === '/api/library/scan') {
    await nd('startScan');
    return sendJson(res, 200, { ok: true });
  }

  // Lecture
  mm = p.match(/^\/api\/stream\/([\w-]+)$/);
  if (m === 'GET' && mm) {
    const params = { id: mm[1] };
    if (cfg.navidrome.transcode) Object.assign(params, { format: cfg.navidrome.transcode, maxBitRate: 320 });
    return proxyBinary(req, res, 'stream', params);
  }
  mm = p.match(/^\/api\/cover\/([\w-]+)$/);
  if (m === 'GET' && mm) {
    return proxyBinary(req, res, 'getCoverArt', { id: mm[1], size: Math.min(Number(q.get('size')) || 300, 1200) });
  }

  // Catalogue à demander (Lidarr / MusicBrainz)
  if (m === 'GET' && p === '/api/search') {
    const term = (q.get('q') || '').trim();
    if (!term) return sendJson(res, 200, { albums: [] });
    return sendJson(res, 200, { albums: await searchRemote(term) });
  }
  if (m === 'POST' && p === '/api/request') {
    const { foreignAlbumId } = await readJson(req);
    if (!foreignAlbumId || typeof foreignAlbumId !== 'string') throw httpError(400, 'foreignAlbumId manquant');
    return sendJson(res, 200, await serialized(() => requestAlbum(foreignAlbumId)));
  }
  if (m === 'POST' && p === '/api/retry') {
    const { albumId } = await readJson(req);
    if (!Number.isInteger(albumId)) throw httpError(400, 'albumId manquant');
    await lidarr('/command', { method: 'POST', body: { name: 'AlbumSearch', albumIds: [albumId] } });
    return sendJson(res, 200, { ok: true });
  }
  if (m === 'GET' && p === '/api/queue') return sendJson(res, 200, await getQueue());

  // Indexeurs (Prowlarr)
  if (m === 'GET' && p === '/api/indexers') return sendJson(res, 200, await getIndexers());
  if (m === 'GET' && p === '/api/indexers/search') {
    const term = (q.get('q') || '').trim();
    if (!term) return sendJson(res, 200, { results: [] });
    return sendJson(res, 200, { results: await prowlarrSearch(term) });
  }

  throw httpError(404, 'Route inconnue');
}

async function serveStatic(res, p) {
  const rel = p === '/' ? '/index.html' : p;
  const file = path.normalize(path.join(PUBLIC, rel));
  if (!file.startsWith(PUBLIC + path.sep)) throw httpError(403, 'Accès refusé');
  try {
    const data = await fs.readFile(file);
    res.writeHead(200, { 'Content-Type': MIME[path.extname(file)] || 'application/octet-stream' });
    res.end(data);
  } catch {
    throw httpError(404, 'Introuvable');
  }
}

const server = http.createServer(async (req, res) => {
  try {
    const url = new URL(req.url, 'http://localhost');
    if (url.pathname === '/healthz') return sendJson(res, 200, { ok: true });
    if (url.pathname.startsWith('/api/')) return await api(req, res, url, url.pathname);
    return await serveStatic(res, url.pathname);
  } catch (e) {
    if (res.headersSent) return res.destroy();
    sendJson(res, e.status || 500, { error: e.message || 'Erreur interne' });
  }
});

server.listen(cfg.port, () => console.log(`MusicHub prêt sur le port ${cfg.port}`));
