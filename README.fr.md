# 🎵 MusicHub

> **Statut : en cours de développement, étape 1 sur 5 terminée** (socle du catalogue). « MusicHub » est un nom provisoire.
>
> 🇬🇧 [Read in English](README.md)

**MusicHub** est une plateforme musicale open source et auto-hébergée : votre propre « Spotify maison », qui tourne sur votre NAS ou votre serveur.

L'objectif est de réunir dans une seule application autonome ce qui demande aujourd'hui plusieurs outils séparés :

- un **catalogue** de toute la musique du monde (artistes, albums, titres, pochettes) ;
- une **bibliothèque musicale et un lecteur web**, avec une interface façon Spotify ;
- un **moteur de téléchargement intégré** : on demande un titre absent du serveur et MusicHub le récupère depuis les sources que vous configurez, sans autre logiciel à installer à côté ;
- une **API Subsonic/OpenSubsonic**, pour que les applis mobiles existantes comme [Symfonium](https://symfonium.app/) continuent de fonctionner.

## État actuel (étape 1)

La première étape est le **socle du catalogue**, écrit en Go avec une interface web Svelte embarquée :

- recherche dans tout le catalogue [MusicBrainz](https://musicbrainz.org/) (artistes, albums, titres) ;
- pages d'artiste avec leur discographie, pages d'album avec la liste des titres et leurs durées ;
- pochettes issues de la [Cover Art Archive](https://coverartarchive.org/), mises en cache sur disque ;
- un cache SQLite local des données du catalogue : les pages déjà visitées restent disponibles quand MusicBrainz est lent ou injoignable (la recherche, elle, a besoin de MusicBrainz).

La lecture et les téléchargements arrivent aux étapes suivantes (voir la feuille de route). L'interface est pour l'instant en français uniquement.

## Lancer en local

Il faut Go 1.26 ou plus récent, plus **ffmpeg** (avec `ffprobe`) et **fpcalc** (Chromaprint) dans le `PATH` : Windows `winget install Gyan.FFmpeg` et `winget install AcoustID.Chromaprint` ; Debian/Ubuntu `sudo apt install ffmpeg libchromaprint-tools` ; macOS `brew install ffmpeg chromaprint`. L'image Docker les contient déjà. L'interface web est embarquée dans le binaire : construisez-la d'abord (voir [Développement](#développement)), puis :

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
| `MUSICHUB_FFMPEG` | `ffmpeg` | Exécutable ffmpeg (nom dans le `PATH` ou chemin complet) |
| `MUSICHUB_FFPROBE` | `ffprobe` | Exécutable ffprobe |
| `MUSICHUB_FPCALC` | `fpcalc` | Exécutable fpcalc (Chromaprint) |

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

La base de données interne et le cache des pochettes sont dans le volume nommé `musichub-data` ; votre dossier de musique est monté depuis `./music`. Le conteneur s'exécute en utilisateur non root (UID/GID 65532). Si vous remplacez le volume nommé par un dossier monté (bind mount) pour `/data`, ce qui est courant sur un NAS, lancez d'abord `chown 65532:65532` sur ce dossier, ou renseignez `user:` dans le fichier compose. `GET /healthz` répond 200 avec `{"ok":true}` quand le serveur tourne, ce qui sert à la supervision.

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

- [x] Catalogue : recherche MusicBrainz, pages d'artiste et d'album, pochettes
- [ ] Scan de votre dossier de musique et lecture dans le navigateur avec file d'attente
- [ ] Comptes : administrateur et utilisateurs, avec leurs propres playlists et favoris
- [ ] Moteur de téléchargement intégré, avec des sources que **vous** configurez : MusicHub n'est livré avec aucune source, et la lecture démarre pendant le téléchargement
- [ ] API Subsonic / OpenSubsonic → compatible Symfonium, Substreamer, DSub…
- [ ] Plus tard : import d'une bibliothèque Spotify, sources directes (hors torrent), interface en anglais

## Feuille de route

Chaque étape aboutit à quelque chose d'utilisable et de testé (voir le [document de conception](docs/conception/v1.md)).

| Étape | Objectif | État |
|------|------|------|
| **1 — Socle et catalogue** | Projet Go, base de données, interface web ; recherche MusicBrainz, pages d'artiste et d'album avec pochettes | ✅ Terminée |
| **2 — Bibliothèque et lecture** | Scan du dossier de musique, lecture dans le navigateur avec lecteur et file d'attente | Prochaine |
| **3 — Comptes** | Connexion, rôles, playlists et favoris par compte, sécurité de base | Prévue |
| **4 — Récupération torrent** | Sources configurées par l'utilisateur, choix de la release, moteur de téléchargement, lecture pendant le téléchargement, aucun double téléchargement | Prévue |
| **5 — API Subsonic** | Les applis mobiles se synchronisent et lisent, y compris les titres pas encore téléchargés | Prévue |

## Mention légale

MusicHub est un outil pour gérer et diffuser **votre propre collection musicale**. Il n'héberge, ne fournit et ne distribue aucun contenu. Chaque utilisateur est responsable du respect du droit d'auteur dans son pays.

## Contribuer

Le projet en est à ses premières étapes : idées, retours et discussions sont les bienvenus ! Voir [CONTRIBUTING.md](CONTRIBUTING.md).

## Licence

[GPL-3.0](LICENSE)
