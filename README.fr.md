# 🎵 MusicHub

> **Statut : concept / en cours de conception.** « MusicHub » est un nom provisoire.
>
> 🇬🇧 [Read in English](README.md)

**MusicHub** est une plateforme musicale open source et auto-hébergée : votre propre « Spotify maison », qui tourne sur votre NAS ou votre serveur.

L'objectif est de réunir dans une seule application ce qui demande aujourd'hui plusieurs outils séparés :

- un **serveur de streaming musical** (dans l'esprit de [Navidrome](https://github.com/navidrome/navidrome)) ;
- un **gestionnaire de bibliothèque** qui suit les artistes et les albums, détecte les sorties manquantes et garde tout organisé (dans l'esprit de [Lidarr](https://github.com/Lidarr/Lidarr) / [Sonarr](https://github.com/Sonarr/Sonarr)) ;
- une **intégration d'indexeurs** (dans l'esprit de [Prowlarr](https://github.com/Prowlarr/Prowlarr)) ;
- un **lecteur web intégré**, et une compatibilité avec les applis mobiles existantes comme [Symfonium](https://symfonium.app/) via l'API Subsonic/OpenSubsonic.

## État actuel (étape 1)

La première étape est le **socle du catalogue**, écrit en Go avec une interface web Svelte embarquée :

- recherche dans tout le catalogue [MusicBrainz](https://musicbrainz.org/) (artistes, albums, titres) ;
- pages d'artiste avec leur discographie, pages d'album avec la liste des titres et leurs durées ;
- pochettes issues de la [Cover Art Archive](https://coverartarchive.org/), mises en cache sur disque ;
- un cache SQLite local des données du catalogue : l'appli continue de fonctionner quand MusicBrainz est lent ou injoignable.

La lecture et les téléchargements arrivent aux étapes suivantes (voir la feuille de route). L'interface est pour l'instant en français uniquement.

## Lancer en local

Il faut Go 1.26 ou plus récent. L'interface web est embarquée dans le binaire : construisez-la d'abord (voir [Développement](#développement)), puis :

```bash
go run ./cmd/musichub
# http://localhost:8080
```

MusicHub se configure avec des **variables d'environnement** (il ne lit pas les fichiers `.env` ; `.env.example` les liste avec leurs valeurs par défaut) :

| Variable | Défaut | Rôle |
|----------|--------|------|
| `MUSICHUB_PORT` | `8080` | Port HTTP |
| `MUSICHUB_DATA` | `./data` (`/data` dans Docker) | Base de données et cache des pochettes |
| `MUSICHUB_MUSIC` | `./music` (`/music` dans Docker) | Dossier de musique (pas encore utilisé) |
| `MUSICHUB_MB_URL` | `https://musicbrainz.org/ws/2` | API MusicBrainz |
| `MUSICHUB_CAA_URL` | `https://coverartarchive.org` | Cover Art Archive |

```bash
# bash
MUSICHUB_PORT=9000 go run ./cmd/musichub
```

```powershell
# PowerShell
$env:MUSICHUB_PORT = "9000"; go run ./cmd/musichub
```

## Lancer avec Docker

```bash
docker compose up -d --build
# http://localhost:8090
```

La base de données interne et le cache des pochettes sont dans le volume nommé `musichub-data` ; votre dossier de musique est monté depuis `./music`. Le conteneur s'exécute en utilisateur non root. `GET /healthz` répond `ok` quand le serveur tourne, ce qui sert à la supervision.

MusicHub n'a **pas encore d'authentification** : utilisez-le sur un réseau de confiance ou derrière un reverse proxy qui gère les accès.

## Développement

```bash
cd web
npm ci
npm run build       # construit l'interface dans internal/web/dist (embarquée par Go)
npm run check       # vérifie les types du code Svelte
npm run dev         # serveur Vite (proxy /api vers localhost:8080)
cd ..

go vet ./...
go test ./...
```

Test de bout en bout (Chromium, sur des données inventées servies par `cmd/fakemb`, sans réseau) :

```bash
cd web
npm run build
npx playwright install chromium
npm run test:e2e
```

## Pourquoi ?

Aujourd'hui, héberger sa musique chez soi demande souvent d'installer, configurer et relier 3 ou 4 applications différentes, chacune avec son interface, sa base de données et ses réglages. MusicHub veut offrir **une seule expérience cohérente** : on ajoute un artiste, et la bibliothèque, les métadonnées et le streaming sont gérés automatiquement, avec une interface façon Spotify par-dessus.

## Fonctionnalités prévues

- [ ] Scan de la bibliothèque musicale (tags, pochettes, arborescence)
- [ ] API Subsonic / OpenSubsonic → compatible Symfonium, DSub, Substreamer, Feishin…
- [ ] Lecteur web intégré (playlists, file d'attente, recherche, mode « radio »)
- [ ] Suivi des artistes / albums et détection des sorties manquantes
- [ ] Intégration d'indexeurs et de clients de téléchargement
- [ ] Enrichissement automatique des métadonnées (MusicBrainz, pochettes)
- [ ] Multi-utilisateur
- [ ] Image Docker pensée pour les NAS (Synology, Unraid, TrueNAS…)

## Feuille de route

| Phase | Objectif |
|------|------|
| **0 — Conception** | Définir l'architecture, la stack technique et le périmètre (ce README) |
| **1 — Cœur streaming** | Scan de la bibliothèque + API Subsonic → utilisable avec Symfonium |
| **2 — Lecteur web** | Interface web native pour écouter et parcourir |
| **3 — Gestion de bibliothèque** | Suivi des artistes, albums manquants, métadonnées |
| **4 — Intégrations** | Indexeurs, clients de téléchargement, notifications |
| **5 — Packaging** | Image Docker, documentation NAS, première version |

## Mention légale

MusicHub est un outil pour gérer et diffuser **votre propre collection musicale**. Il n'héberge, ne fournit et ne distribue aucun contenu. Chaque utilisateur est responsable du respect du droit d'auteur dans son pays.

## Contribuer

Le projet démarre tout juste : idées, retours et discussions sont les bienvenus ! Voir [CONTRIBUTING.md](CONTRIBUTING.md).

## Licence

[GPL-3.0](LICENSE)
