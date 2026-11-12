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

## Preuve de concept

Ce dépôt contient pour l'instant une petite **preuve de concept** qui montre à quoi ressemble l'idée : une seule page pour chercher, demander et écouter de la musique, en pilotant des instances Navidrome, Lidarr et Prowlarr existantes. C'est une démo, pas l'architecture finale. Voir [POC.md](POC.md) pour la lancer.

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
