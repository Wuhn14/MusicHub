# 🎵 MusicHub

> **Status: early concept / work in progress.** "MusicHub" is a provisional name.
>
> 🇫🇷 [Lire en français](README.fr.md)

**MusicHub** is a self-hosted, open-source music platform: your own "home Spotify", running on your NAS or home server.

It aims to bring together, in a single app, what today requires several separate tools:

- a **music streaming server** (in the spirit of [Navidrome](https://github.com/navidrome/navidrome)),
- a **library manager** that tracks artists and albums, finds missing releases and keeps everything organized (in the spirit of [Lidarr](https://github.com/Lidarr/Lidarr) / [Sonarr](https://github.com/Sonarr/Sonarr)),
- **indexer integration** (in the spirit of [Prowlarr](https://github.com/Prowlarr/Prowlarr)),
- a **built-in web player**, plus compatibility with existing mobile clients such as [Symfonium](https://symfonium.app/) through the Subsonic/OpenSubsonic API.

## Current state (step 1)

The first step is the **catalogue foundation**, written in Go with an embedded Svelte web interface:

- search the whole [MusicBrainz](https://musicbrainz.org/) catalogue (artists, albums, tracks),
- artist pages with their discography, and album pages with the track list and durations,
- covers from the [Cover Art Archive](https://coverartarchive.org/), cached on disk,
- a local SQLite cache of catalogue data, so the app keeps working when MusicBrainz is slow or unreachable.

Playback and downloads come in later steps (see the roadmap). The interface is currently in French only.

## Run locally

You need Go 1.26 or newer. The web interface is embedded in the binary, so build it first (see [Development](#development)), then:

```bash
go run ./cmd/musichub
# http://localhost:8080
```

MusicHub is configured with **environment variables** (it does not read `.env` files; `.env.example` lists them with their defaults):

| Variable | Default | Purpose |
|----------|---------|---------|
| `MUSICHUB_PORT` | `8080` | HTTP port |
| `MUSICHUB_DATA` | `./data` (`/data` in Docker) | Database and cover cache |
| `MUSICHUB_MUSIC` | `./music` (`/music` in Docker) | Music folder (not used yet) |
| `MUSICHUB_MB_URL` | `https://musicbrainz.org/ws/2` | MusicBrainz API |
| `MUSICHUB_CAA_URL` | `https://coverartarchive.org` | Cover Art Archive |

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

The internal database and cover cache live in the named volume `musichub-data`; your music folder is mounted from `./music`. The container runs as a non-root user. `GET /healthz` returns `ok` when the server is up, which you can use for monitoring.

MusicHub has **no authentication yet**: run it on a trusted network or behind a reverse proxy that handles access control.

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

- [ ] Music library scanning (tags, covers, folder structure)
- [ ] Subsonic / OpenSubsonic API → works with Symfonium, DSub, Substreamer, Feishin…
- [ ] Built-in web player (playlists, queue, search, "radio" mode)
- [ ] Artist / album monitoring and missing-release detection
- [ ] Indexer and download-client integration
- [ ] Automatic metadata enrichment (MusicBrainz, cover art)
- [ ] Multi-user support
- [ ] Docker image designed for NAS (Synology, Unraid, TrueNAS…)

## Roadmap

| Phase | Goal |
|------|------|
| **0 — Design** | Define the architecture, tech stack and scope (this README) |
| **1 — Streaming core** | Library scan + Subsonic API → usable with Symfonium |
| **2 — Web player** | Native web interface for listening and browsing |
| **3 — Library management** | Artist monitoring, missing albums, metadata |
| **4 — Integrations** | Indexers, download clients, notifications |
| **5 — Packaging** | Docker image, NAS documentation, first release |

## Legal notice

MusicHub is a tool for managing and streaming **your own music collection**. It does not host, provide or distribute any content. Users are responsible for complying with the copyright laws of their country.

## Contributing

The project is just getting started — ideas, feedback and discussions are very welcome! See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[GPL-3.0](LICENSE)
