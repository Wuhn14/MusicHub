# MusicHub — preuve de concept

Preuve de concept d'un « Spotify maison » : une seule page pour **chercher**, **demander** et **écouter** de la musique, en réunissant des briques auto-hébergées.

- **Navidrome** diffuse la bibliothèque (lecteur intégré, API Subsonic).
- **Lidarr** ajoute l'album demandé et lance le téléchargement.
- **Prowlarr** fournit les indexeurs (état et test de recherche dans l'onglet *Sources*).

Quand les téléchargements se terminent, MusicHub demande à Navidrome d'analyser la bibliothèque : l'album apparaît tout seul.

> Projet de démonstration, sans authentification intégrée. MusicHub ne fournit aucune musique : il pilote des outils que tu configures toi-même. Vérifie que tes téléchargements restent légaux dans ton pays.

## Fonctionnement

1. La recherche interroge Navidrome (ce que tu as déjà) et Lidarr/MusicBrainz (ce que tu peux demander).
2. « Demander » ajoute l'artiste dans Lidarr (sans rien télécharger d'autre), surveille cet album et lance sa recherche via les indexeurs de Prowlarr.
3. L'onglet *Téléchargements* suit la progression et permet de relancer une recherche sans résultat.

Aucune dépendance : Node 20 ou plus suffit (serveur HTTP natif, interface en JavaScript pur).

## Lancer en local

```bash
cp .env.example .env     # puis remplis les adresses, les clés d'API et le compte Navidrome
node --env-file=.env server.js
```

Ouvre ensuite http://localhost:8080.

## Lancer avec Docker

```bash
cp .env.example .env
docker compose up -d --build
```

L'interface est alors sur le port 8090.

## Prérequis côté Lidarr

- Un dossier racine qui correspond au **même dossier** que celui lu par Navidrome.
- Un client de téléchargement (qBittorrent, SABnzbd…) et les indexeurs de Prowlarr synchronisés avec Lidarr.

## Configuration

Toutes les variables sont décrites dans `.env.example`. Les clés d'API se trouvent dans *Settings > General* de Lidarr et de Prowlarr.

## Sécurité

MusicHub n'a pas d'authentification et détient les clés de tes services : ne l'expose pas directement sur Internet. Place-le derrière un reverse proxy avec authentification (voir `musichub.subdomain.conf.sample` pour SWAG + Authelia) ou garde-le en réseau local / VPN. Ne publie jamais ton fichier `.env`.
