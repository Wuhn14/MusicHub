# 🎵 MusicHub

> **Status: work in progress, step 2 of 5 done** (catalogue, library and playback). "MusicHub" is a provisional name.
>
> 🇫🇷 [Lire en français](README.fr.md)

**MusicHub** is a self-hosted, open-source music platform: your own "home Spotify", running on your NAS or home server.

It aims to bring together, in a single self-contained app, what today requires several separate tools:

- a **catalogue** of all the music in the world (artists, albums, tracks, covers),
- a **music library and web player**, with a Spotify-like interface,
- a **built-in download engine**: ask for a track that is not on your server and MusicHub fetches it from the sources you configure, with no other software to install alongside,
- a **Subsonic/OpenSubsonic API**, so existing mobile clients such as [Symfonium](https://symfonium.app/) keep working.

## Current state (steps 1 and 2)

MusicHub is written in Go with an embedded Svelte web interface.

**Step 1: catalogue**

- search the whole [MusicBrainz](https://musicbrainz.org/) catalogue (artists, albums, tracks),
- artist pages with their discography, and album pages with the track list and durations,
- covers from the [Cover Art Archive](https://coverartarchive.org/), cached on disk,
- a local SQLite cache of catalogue data: pages you have already visited stay available when MusicBrainz is slow or unreachable (searching still needs MusicBrainz).

**Step 2: library and playback**

- **Library scan** of your music folder (MP3, FLAC, M4A/AAC/ALAC, OGG Vorbis, Opus, WAV, AIFF), by album, artist or track; unreadable files are flagged and the scan goes on.
- **Upload from the browser or a phone**: drop files or a folder on the "Ajouter" page.
- **Automatic identification** of uploaded files: MusicBrainz recording id in the tags, then [AcoustID](https://acoustid.org/) audio fingerprints, then a MusicBrainz search on the tags.
- An **"À identifier" screen** for the files that could not be matched: pick a proposed recording, search the catalogue, or create a **local track** (music that is not in the catalogue, such as your own recordings).
- **Organizing**: identified uploads are moved into your music folder as `Artist/Album (year)/NN - Title`, with their tags written. Files you drop in the music folder yourself are never moved or modified unless you enable it in "Réglages".
- **Player** with a queue, lock-screen controls (Media Session) and optional on-the-fly MP3 conversion (320, 192 or 128 kbps) to save bandwidth.
- A **settings page** ("Réglages"): AcoustID key, scan interval, organizing and tag-writing options, duplicate handling, upload size limit.

Downloads come in later steps (see the roadmap). The interface is currently in French only.

**AcoustID key (optional).** Fingerprint identification needs a free AcoustID application key. Each user creates their own on the AcoustID website and enters it in "Réglages"; MusicHub never ships one. Without a key, identification relies on tags and the MusicBrainz search.

## Run locally

You need Go 1.26 or newer, plus **ffmpeg** (with `ffprobe`) and **fpcalc** (Chromaprint) on your `PATH`: Windows `winget install Gyan.FFmpeg` and `winget install AcoustID.Chromaprint`; Debian/Ubuntu `sudo apt install ffmpeg libchromaprint-tools`; macOS `brew install ffmpeg chromaprint`. The Docker image already includes them. The web interface is embedded in the binary, so build it first (see [Development](#development)), then:

```bash
go run ./cmd/musichub
# http://localhost:8080
```

MusicHub is configured with **environment variables** (it does not read `.env` files; `.env.example` lists them with their defaults):

| Variable | Default | Purpose |
|----------|---------|---------|
| `MUSICHUB_PORT` | `8080` | HTTP port |
| `MUSICHUB_DATA` | `./data` (`/data` in Docker) | Database, caches and incoming uploads |
| `MUSICHUB_MUSIC` | `./music` (`/music` in Docker) | Music folder (scanned; identified uploads are moved here) |
| `MUSICHUB_MB_URL` | `https://musicbrainz.org/ws/2` | MusicBrainz API |
| `MUSICHUB_CAA_URL` | `https://coverartarchive.org` | Cover Art Archive |
| `MUSICHUB_ACOUSTID_URL` | `https://api.acoustid.org/v2` | AcoustID API |
| `MUSICHUB_FFMPEG` | `ffmpeg` | ffmpeg executable (name in `PATH` or full path) |
| `MUSICHUB_FFPROBE` | `ffprobe` | ffprobe executable |
| `MUSICHUB_FPCALC` | `fpcalc` | fpcalc (Chromaprint) executable |

```bash
# bash
MUSICHUB_PORT=9000 go run ./cmd/musichub
```

```powershell
# PowerShell
$env:MUSICHUB_PORT = "9000"; go run ./cmd/musichub
```

## Run with Docker

```bash
docker compose up -d --build
# http://localhost:8090
```

The image includes ffmpeg and fpcalc. Two volumes are used: `/data` (named volume `musichub-data`: database, caches and **incoming uploads**) and `/music` (your music folder, mounted from `./music`). Uploaded files wait in `/data` until they are identified, then are moved into `/music`; files you drop in `/music` yourself are never moved or modified unless you enable it in "Réglages". The container runs as a non-root user (UID/GID 65532): if you replace the named volume with a bind mount for `/data` (common on a NAS), run `chown 65532:65532` on that folder first, make sure that user can also write to `/music`, or set `user:` in the compose file. `GET /healthz` returns HTTP 200 with `{"ok":true}` when the server is up, which you can use for monitoring.

MusicHub has **no authentication yet**: uploads and settings are open to anyone who can reach the app. Run it on a trusted network or VPN, or behind a reverse proxy that handles access control.

## Development

```bash
cd web
npm ci
npm run build       # builds the UI into internal/web/dist (embedded by Go)
npm run check       # type-check the Svelte code
npm run dev         # Vite dev server (proxies /api to localhost:8080)
cd ..

go vet ./...
go test ./...
```

End-to-end test (Chromium, against invented data served by `cmd/fakemb`, no network needed):

```bash
cd web
npm run build
npx playwright install chromium
npm run test:e2e
```

## Why?

Self-hosting music today usually means installing, configuring and wiring together 3 or 4 different applications, each with its own interface, database and settings. MusicHub's goal is a **single, coherent experience**: add an artist, and the library, metadata, and streaming are handled for you, with a Spotify-like interface on top.

## Planned features

- [x] Catalogue: MusicBrainz search, artist and album pages, covers
- [x] Library scan of your music folder, upload and automatic identification, in-browser playback with a queue
- [ ] Accounts: administrator and users, with their own playlists and favourites
- [ ] Built-in download engine, with sources **you** configure: MusicHub ships with no source, and starts playing while the download is still running
- [ ] Subsonic / OpenSubsonic API → works with Symfonium, Substreamer, DSub…
- [ ] Later: importing a Spotify library, direct (non-torrent) sources, English interface

## Roadmap

Each step ends with something usable and tested (see the [design document](docs/conception/v1.md), in French).

| Step | Goal | Status |
|------|------|--------|
| **1 — Foundation and catalogue** | Go project, database, web interface; MusicBrainz search, artist and album pages with covers | ✅ Done |
| **2 — Library and playback** | Scan the music folder, upload and identify files, play in the browser with a player and a queue | ✅ Done |
| **3 — Accounts** | Sign-in, roles, per-account playlists and favourites, basic security | Planned |
| **4 — Torrent retrieval** | User-configured sources, release choice, download engine, play while downloading, no duplicate downloads | Planned |
| **5 — Subsonic API** | Mobile clients can sync and play, including tracks not yet downloaded | Planned |

## Legal notice

MusicHub is a tool for managing and streaming **your own music collection**. It does not host, provide or distribute any content. Users are responsible for complying with the copyright laws of their country.

## Contributing

The project is in its early steps: ideas, feedback and discussions are very welcome! See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[GPL-3.0](LICENSE)
