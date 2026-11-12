/* MusicHub : interface (JavaScript pur, sans dépendance) */

const $ = (s) => document.querySelector(s);
const viewEl = $('#view');
const audio = $('#audio');

/* ---------- Outils ---------- */

function h(tag, props = {}, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(props)) {
    if (v == null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else if (v === true) el.setAttribute(k, '');
    else el.setAttribute(k, v);
  }
  for (const kid of kids.flat()) {
    if (kid == null || kid === false) continue;
    el.append(kid.nodeType ? kid : document.createTextNode(String(kid)));
  }
  return el;
}

async function api(path, opts = {}) {
  const init = { ...opts };
  if (opts.body) {
    init.method = init.method || 'POST';
    init.headers = { 'Content-Type': 'application/json' };
    init.body = JSON.stringify(opts.body);
  }
  const r = await fetch('/api' + path, init);
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(j.error || `Erreur ${r.status}`);
  return j;
}

const fmtTime = (s) => {
  s = Math.max(0, Math.floor(s || 0));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
};
const fmtSize = (b) => (b >= 1e9 ? (b / 1e9).toFixed(1) + ' Go' : b >= 1e6 ? Math.round(b / 1e6) + ' Mo' : Math.round(b / 1e3) + ' Ko');

let toastTimer;
function toast(msg, isError = false) {
  const t = $('#toast');
  t.textContent = msg;
  t.className = 'show' + (isError ? ' err' : '');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => (t.className = ''), isError ? 6000 : 3500);
}

function hue(str) {
  let n = 0;
  for (const c of str || '?') n = (n * 31 + c.charCodeAt(0)) % 360;
  return n;
}

// Pochette avec repli : une couleur et l'initiale si l'image est absente.
function cover(src, label, cls = 'art') {
  const box = h('div', { class: cls });
  const fallback = () => {
    box.replaceChildren(h('div', { class: 'ph', style: `width:100%;height:100%;background:hsl(${hue(label)} 35% 32%)` }, (label || '?')[0].toUpperCase()));
  };
  if (!src) {
    fallback();
  } else {
    const img = h('img', { src, alt: '', loading: 'lazy', referrerpolicy: 'no-referrer' });
    img.addEventListener('error', fallback);
    box.append(img);
  }
  return box;
}
const localCover = (id, label, cls) => cover(id ? `/api/cover/${encodeURIComponent(id)}?size=300` : null, label, cls);

/* ---------- Lecteur ---------- */

const player = { list: [], i: -1 };

function playList(list, i) {
  player.list = list;
  player.i = i;
  loadCurrent();
}

function loadCurrent() {
  const s = player.list[player.i];
  if (!s) return;
  audio.src = `/api/stream/${encodeURIComponent(s.id)}`;
  audio.play().catch(() => {});
  $('#ptitle').textContent = s.title;
  $('#partist').textContent = [s.artist, s.album].filter(Boolean).join(' · ');
  const img = $('#pcover');
  const fb = $('#pcoverFallback');
  img.hidden = false;
  fb.hidden = true;
  img.onerror = () => {
    img.hidden = true;
    fb.hidden = false;
  };
  img.src = `/api/cover/${encodeURIComponent(s.coverArt)}?size=200`;
  fb.style.background = `hsl(${hue(s.album)} 35% 32%)`;
  fb.textContent = (s.album || '?')[0].toUpperCase();
  document.querySelectorAll('.row[data-song]').forEach((r) => r.classList.toggle('playing', r.dataset.song === s.id));
  if ('mediaSession' in navigator) {
    navigator.mediaSession.metadata = new MediaMetadata({
      title: s.title,
      artist: s.artist || '',
      album: s.album || '',
      artwork: [{ src: `/api/cover/${encodeURIComponent(s.coverArt)}?size=300`, sizes: '300x300' }],
    });
  }
}

const step = (d) => {
  const n = player.i + d;
  if (n >= 0 && n < player.list.length) {
    player.i = n;
    loadCurrent();
  }
};
const toggle = () => (audio.paused ? audio.play().catch(() => {}) : audio.pause());

$('#play').addEventListener('click', () => (player.i < 0 ? null : toggle()));
$('#prev').addEventListener('click', () => (audio.currentTime > 4 ? (audio.currentTime = 0) : step(-1)));
$('#next').addEventListener('click', () => step(1));
$('#vol').addEventListener('input', (e) => (audio.volume = e.target.value / 100));
audio.volume = 0.8;

let seeking = false;
$('#seek').addEventListener('input', () => (seeking = true));
$('#seek').addEventListener('change', (e) => {
  if (audio.duration) audio.currentTime = (e.target.value / 1000) * audio.duration;
  seeking = false;
});
audio.addEventListener('timeupdate', () => {
  $('#tcur').textContent = fmtTime(audio.currentTime);
  if (audio.duration && !seeking) $('#seek').value = Math.round((audio.currentTime / audio.duration) * 1000);
});
audio.addEventListener('loadedmetadata', () => ($('#tdur').textContent = fmtTime(audio.duration)));
audio.addEventListener('play', () => {
  $('#play').textContent = '⏸';
  $('#play').setAttribute('aria-label', 'Pause');
  $('#player').classList.add('playing-now');
});
audio.addEventListener('pause', () => {
  $('#play').textContent = '▶';
  $('#play').setAttribute('aria-label', 'Lecture');
  $('#player').classList.remove('playing-now');
});
audio.addEventListener('ended', () => (player.i < player.list.length - 1 ? step(1) : null));
audio.addEventListener('error', () => player.i >= 0 && toast('Lecture impossible pour ce titre', true));

if ('mediaSession' in navigator) {
  navigator.mediaSession.setActionHandler('play', toggle);
  navigator.mediaSession.setActionHandler('pause', toggle);
  navigator.mediaSession.setActionHandler('previoustrack', () => step(-1));
  navigator.mediaSession.setActionHandler('nexttrack', () => step(1));
}

/* ---------- Éléments communs ---------- */

function albumTile(a) {
  const art = h('button', { class: 'art', 'aria-label': `Ouvrir ${a.name}`, onclick: () => openAlbum(a.id) }, localCover(a.coverArt, a.name, 'fill'));
  const wrap = h('div', { class: 'tile' }, art, h('div', { class: 'name', title: a.name }, a.name), h('div', { class: 'by' }, [a.artist, a.year].filter(Boolean).join(' · ')));
  return wrap;
}

function songRows(songs, { numbered = false, showAlbum = false } = {}) {
  return h(
    'div',
    { class: 'rows' },
    songs.map((s, i) =>
      h(
        'button',
        { class: 'row' + (player.list[player.i]?.id === s.id ? ' playing' : ''), 'data-song': s.id, onclick: () => playList(songs, i) },
        h('span', { class: 'num' }, numbered ? s.track || i + 1 : '▶'),
        h('span', { class: 't' }, s.title, h('small', {}, showAlbum ? [s.artist, s.album].filter(Boolean).join(' · ') : s.artist)),
        h('span', { class: 'd' }, fmtTime(s.duration)),
      ),
    ),
  );
}

async function openAlbum(id) {
  const dlg = $('#sheet');
  dlg.replaceChildren(h('div', { class: 'empty', style: 'padding:24px' }, 'Chargement…'));
  dlg.showModal();
  try {
    const a = await api(`/library/album/${encodeURIComponent(id)}`);
    const songs = a.songs;
    dlg.replaceChildren(
      h(
        'div',
        { class: 'hd' },
        localCover(a.coverArt, a.name, 'art'),
        h(
          'div',
          {},
          h('h2', {}, a.name),
          h('div', { class: 'sub', style: 'margin:0' }, [a.artist, a.year, `${songs.length} titres`, fmtTime(a.duration)].filter(Boolean).join(' · ')),
          h('div', { class: 'acts' }, h('button', { class: 'btn', onclick: () => { playList(songs, 0); dlg.close(); } }, 'Tout lire'), h('button', { class: 'btn ghost', onclick: () => dlg.close() }, 'Fermer')),
        ),
      ),
      songRows(songs, { numbered: true }),
    );
  } catch (e) {
    dlg.replaceChildren(h('div', { class: 'error', style: 'padding:24px' }, e.message), h('div', { style: 'padding:0 24px 24px' }, h('button', { class: 'btn ghost', onclick: () => dlg.close() }, 'Fermer')));
  }
}
$('#sheet').addEventListener('click', (e) => e.target === e.currentTarget && e.currentTarget.close());

/* ---------- Vues ---------- */

let renderId = 0;
let poll = null;
let homeType = 'newest';

const loading = () => h('div', { class: 'empty' }, 'Chargement…');

async function viewHome() {
  const id = ++renderId;
  const seg = h(
    'div',
    { class: 'seg', role: 'group', 'aria-label': 'Tri' },
    [
      ['newest', 'Récents'],
      ['alphabeticalByName', 'A à Z'],
      ['random', 'Au hasard'],
    ].map(([t, label]) =>
      h('button', { class: t === homeType ? 'on' : '', onclick: () => { homeType = t; viewHome(); } }, label),
    ),
  );
  viewEl.replaceChildren(h('h2', {}, 'Ta bibliothèque'), seg, loading());
  try {
    const { albums } = await api(`/library/albums?type=${homeType}&size=80`);
    if (id !== renderId) return;
    const body = albums.length
      ? h('div', { class: 'grid' }, albums.map(albumTile))
      : h('div', { class: 'empty' }, "Ta bibliothèque est vide pour l'instant. Utilise la recherche pour demander ton premier album : il sera téléchargé puis apparaîtra ici.");
    viewEl.replaceChildren(h('h2', {}, 'Ta bibliothèque'), seg, body);
  } catch (e) {
    if (id === renderId) viewEl.replaceChildren(h('h2', {}, 'Ta bibliothèque'), h('div', { class: 'error' }, e.message));
  }
}

function remoteTile(a) {
  const action = h('div', {});
  const askBtn = h('button', { class: 'btn small' }, 'Demander');
  askBtn.addEventListener('click', async () => {
    askBtn.disabled = true;
    askBtn.textContent = 'Demande en cours…';
    try {
      const r = await api('/request', { body: { foreignAlbumId: a.foreignAlbumId } });
      if (r.already) {
        toast(`« ${a.title} » est déjà dans ta bibliothèque`);
        action.replaceChildren(h('span', { class: 'badge' }, 'Dans ta bibliothèque'));
      } else {
        toast(`« ${a.title} » est demandé. Il arrive dans ta bibliothèque dès qu'il est téléchargé.`);
        action.replaceChildren(h('span', { class: 'badge wait' }, 'Demandé, recherche en cours'));
        refreshQueueCount();
      }
    } catch (e) {
      toast(e.message, true);
      askBtn.disabled = false;
      askBtn.textContent = 'Demander';
    }
  });
  if (a.complete) action.append(h('span', { class: 'badge' }, 'Dans ta bibliothèque'));
  else if (a.libraryId && a.monitored) action.append(h('span', { class: 'badge wait' }, 'Déjà demandé'));
  else action.append(askBtn);

  return h(
    'div',
    { class: 'tile' },
    cover(a.cover, a.title),
    h('div', { class: 'name', title: a.title }, a.title),
    h('div', { class: 'by' }, [a.artist, a.year, a.type].filter(Boolean).join(' · ')),
    action,
  );
}

async function viewSearch(q) {
  const id = ++renderId;
  if (!q) {
    viewEl.replaceChildren(h('h2', {}, 'Recherche'), h('div', { class: 'empty' }, "Tape un artiste, un album ou un titre. Ce qui est déjà chez toi se lit tout de suite ; le reste, tu peux le demander."));
    return;
  }
  viewEl.replaceChildren(h('h2', {}, `Résultats pour « ${q} »`), loading());
  const enc = encodeURIComponent(q);
  const [local, remote] = await Promise.allSettled([api(`/library/search?q=${enc}`), api(`/search?q=${enc}`)]);
  if (id !== renderId) return;

  const out = [h('h2', {}, `Résultats pour « ${q} »`)];

  out.push(h('h3', {}, 'Dans ta bibliothèque'));
  if (local.status === 'rejected') out.push(h('div', { class: 'error' }, local.reason.message));
  else if (!local.value.albums.length && !local.value.songs.length) out.push(h('div', { class: 'empty' }, "Rien de tel chez toi pour l'instant."));
  else {
    if (local.value.albums.length) out.push(h('div', { class: 'grid' }, local.value.albums.map(albumTile)));
    if (local.value.songs.length) out.push(h('h3', {}, 'Titres'), songRows(local.value.songs.slice(0, 12), { showAlbum: true }));
  }

  out.push(h('h3', {}, 'À demander'));
  out.push(h('p', { class: 'sub' }, 'Albums du catalogue MusicBrainz. Une demande lance le téléchargement via Lidarr et tes indexeurs.'));
  if (remote.status === 'rejected') out.push(h('div', { class: 'error' }, remote.reason.message));
  else if (!remote.value.albums.length) out.push(h('div', { class: 'empty' }, 'Aucun album trouvé dans le catalogue.'));
  else out.push(h('div', { class: 'grid' }, remote.value.albums.map(remoteTile)));

  viewEl.replaceChildren(...out);
}

async function renderQueue(id) {
  try {
    const { downloading, waiting } = await api('/queue');
    if (id !== renderId) return;
    setCount(downloading.length);
    const out = [h('h2', {}, 'Téléchargements')];

    out.push(h('div', { style: 'margin-bottom:8px' }, h('button', { class: 'btn ghost small', onclick: async () => {
      try { await api('/library/scan', { method: 'POST' }); toast('Navidrome analyse ta bibliothèque'); } catch (e) { toast(e.message, true); }
    } }, 'Actualiser la bibliothèque')));

    out.push(h('h3', {}, 'En cours'));
    if (!downloading.length) out.push(h('div', { class: 'empty' }, 'Aucun téléchargement en cours.'));
    for (const j of downloading) {
      out.push(
        h(
          'div',
          { class: 'job' },
          h('div', { class: 'head' }, h('span', { class: 'name' }, [j.artist, j.album].filter(Boolean).join(' : ')), h('span', { class: 'info' }, j.state === 'downloading' ? `${j.progress} %` : j.state)),
          h('div', { class: 'bar' }, h('i', { style: `width:${j.progress}%` })),
          j.timeleft && h('div', { class: 'info' }, `Temps restant : ${j.timeleft}`),
          j.problem && h('div', { class: 'warn' }, j.problem),
        ),
      );
    }

    out.push(h('h3', {}, 'Sans source pour le moment'));
    out.push(h('p', { class: 'sub' }, "Albums demandés qu'aucun indexeur n'a encore fournis. Relance la recherche ou ajoute des indexeurs dans Prowlarr."));
    if (!waiting.length) out.push(h('div', { class: 'empty' }, 'Rien en attente.'));
    for (const w of waiting) {
      out.push(
        h(
          'div',
          { class: 'job' },
          h('div', { class: 'head' }, h('span', { class: 'name' }, [w.artist, w.album].filter(Boolean).join(' : ')), h('span', { class: 'info' }, w.year || '')),
          h('div', {}, h('button', { class: 'btn ghost small', onclick: async (e) => {
            e.target.disabled = true;
            try { await api('/retry', { body: { albumId: w.albumId } }); toast('Recherche relancée'); } catch (err) { toast(err.message, true); }
            e.target.disabled = false;
          } }, 'Relancer la recherche')),
        ),
      );
    }
    viewEl.replaceChildren(...out);
  } catch (e) {
    if (id === renderId) viewEl.replaceChildren(h('h2', {}, 'Téléchargements'), h('div', { class: 'error' }, e.message));
  }
}

function viewQueue() {
  const id = ++renderId;
  viewEl.replaceChildren(h('h2', {}, 'Téléchargements'), loading());
  renderQueue(id);
  poll = setInterval(() => renderQueue(id), 5000);
}

async function viewSources() {
  const id = ++renderId;
  viewEl.replaceChildren(h('h2', {}, 'Sources'), loading());
  const [st, ix] = await Promise.allSettled([api('/status'), api('/indexers')]);
  if (id !== renderId) return;
  const out = [h('h2', {}, 'Sources')];

  out.push(h('h3', {}, 'Services'));
  if (st.status === 'rejected') out.push(h('div', { class: 'error' }, st.reason.message));
  else {
    const names = { lidarr: 'Lidarr', prowlarr: 'Prowlarr', navidrome: 'Navidrome' };
    const roles = { lidarr: 'Gère la bibliothèque et lance les téléchargements', prowlarr: 'Fournit les indexeurs', navidrome: 'Diffuse la musique' };
    out.push(h('div', { class: 'cards' }, Object.keys(names).map((k) => {
      const s = st.value[k];
      return h('div', { class: 'card' }, h('div', { class: 'k' }, roles[k]), h('div', { class: 'v' }, names[k]), s.ok ? h('span', { class: 'pill on' }, `Connecté${s.version ? ' · v' + s.version : ''}`) : h('span', { class: 'pill bad' }, s.error));
    })));
  }

  out.push(h('h3', {}, 'Indexeurs Prowlarr'));
  if (ix.status === 'rejected') out.push(h('div', { class: 'error' }, ix.reason.message));
  else {
    const { indexers, health } = ix.value;
    for (const hl of health) out.push(h('div', { class: 'job' }, h('div', { class: 'warn' }, hl.message)));
    if (!indexers.length) out.push(h('div', { class: 'empty' }, "Aucun indexeur pour l'instant. Ajoute-en dans Prowlarr : sans indexeur, aucune demande ne peut aboutir."));
    else out.push(h('div', { class: 'cards' }, indexers.map((i) => h('div', { class: 'card' }, h('div', { class: 'v' }, i.name), h('span', { class: 'pill' }, i.protocol), i.enable ? (i.failing ? h('span', { class: 'pill bad' }, 'En erreur') : h('span', { class: 'pill on' }, 'Actif')) : h('span', { class: 'pill' }, 'Désactivé')))));

    const resultBox = h('div', {});
    const input = h('input', { type: 'search', placeholder: 'Tester une recherche, par exemple un nom d’album', 'aria-label': 'Tester les indexeurs' });
    const go = async (e) => {
      e.preventDefault();
      const term = input.value.trim();
      if (!term) return;
      resultBox.replaceChildren(loading());
      try {
        const { results } = await api(`/indexers/search?q=${encodeURIComponent(term)}`);
        resultBox.replaceChildren(results.length
          ? h('table', { class: 'res' }, results.map((r) => h('tr', {}, h('td', {}, r.title), h('td', {}, r.indexer), h('td', { class: 'n' }, fmtSize(r.size)), h('td', { class: 'n' }, r.seeders != null ? `${r.seeders} sources` : r.protocol))))
          : h('div', { class: 'empty' }, 'Aucun résultat sur tes indexeurs.'));
      } catch (err) {
        resultBox.replaceChildren(h('div', { class: 'error' }, err.message));
      }
    };
    out.push(h('h3', {}, 'Tester les indexeurs'), h('form', { class: 'testform', onsubmit: go }, input, h('button', { class: 'btn', type: 'submit' }, 'Chercher')), resultBox);
  }
  viewEl.replaceChildren(...out);
}

/* ---------- Navigation ---------- */

function setCount(n) {
  const c = $('#qcount');
  c.hidden = !n;
  c.textContent = n;
}
async function refreshQueueCount() {
  try { setCount((await api('/queue')).downloading.length); } catch { /* ignoré */ }
}

async function refreshServices() {
  try {
    const s = await api('/status');
    const names = { lidarr: 'Lidarr', prowlarr: 'Prowlarr', navidrome: 'Navidrome' };
    $('#svc').replaceChildren(...Object.keys(names).map((k) => h('div', { title: s[k].ok ? '' : s[k].error }, h('span', { class: 'dot ' + (s[k].ok ? 'ok' : 'bad') }), names[k])));
  } catch { /* ignoré */ }
}

let searchTerm = '';
function render() {
  clearInterval(poll);
  const v = (location.hash || '#home').slice(1);
  document.querySelectorAll('#nav button').forEach((b) => b.classList.toggle('on', b.dataset.view === v));
  if (v === 'search') viewSearch(searchTerm);
  else if (v === 'queue') viewQueue();
  else if (v === 'sources') viewSources();
  else viewHome();
}

$('#nav').addEventListener('click', (e) => {
  const b = e.target.closest('button[data-view]');
  if (!b) return;
  if (location.hash === '#' + b.dataset.view) render();
  else location.hash = b.dataset.view;
});
window.addEventListener('hashchange', render);

let typing;
const q = $('#q');
const runSearch = () => {
  searchTerm = q.value.trim();
  if (location.hash === '#search') render();
  else location.hash = 'search';
};
q.addEventListener('input', () => {
  clearTimeout(typing);
  typing = setTimeout(runSearch, 450);
});
q.addEventListener('keydown', (e) => {
  if (e.key === 'Enter') {
    clearTimeout(typing);
    runSearch();
  }
});

render();
refreshServices();
refreshQueueCount();
setInterval(refreshServices, 60000);
setInterval(refreshQueueCount, 15000);
