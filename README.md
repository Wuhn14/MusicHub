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
