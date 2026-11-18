// Minimal router on top of the History API: links are plain <a href>
// elements, and a single document-level click handler turns same-origin
// navigations into pushState calls.

export type Route =
  | { name: 'home' }
  | { name: 'search'; query: string }
  | { name: 'artist'; id: string }
  | { name: 'album'; id: string }
  | { name: 'notFound' };

const artistPath = /^\/artist\/([^/]+)\/?$/;
const albumPath = /^\/album\/([^/]+)\/?$/;

function decode(segment: string): string {
  try {
    return decodeURIComponent(segment);
  } catch {
    return segment;
  }
}

/** Turns a location into a route. */
export function parseRoute(loc: { pathname: string; search: string }): Route {
  const path = loc.pathname;
  if (path === '/' || path === '') {
    return { name: 'home' };
  }
  if (path === '/search' || path === '/search/') {
    return { name: 'search', query: new URLSearchParams(loc.search).get('q') ?? '' };
  }
  let m = artistPath.exec(path);
  if (m) {
    return { name: 'artist', id: decode(m[1]) };
  }
  m = albumPath.exec(path);
  if (m) {
    return { name: 'album', id: decode(m[1]) };
  }
  return { name: 'notFound' };
}

type Listener = () => void;
const listeners = new Set<Listener>();

/** Pushes a new history entry and renders the matching page. */
export function navigate(href: string): void {
  history.pushState(null, '', href);
  for (const fn of listeners) {
    fn();
  }
}

/**
 * Updates the query string of the current entry without re-rendering, so
 * that reloading or coming back to the search page restores the query.
 */
export function replaceQuery(q: string): void {
  const url = new URL(location.href);
  if (q) {
    url.searchParams.set('q', q);
  } else {
    url.searchParams.delete('q');
  }
  if (url.href !== location.href) {
    history.replaceState(history.state, '', url);
  }
}

function onClick(event: MouseEvent): void {
  if (
    event.defaultPrevented ||
    event.button !== 0 ||
    event.metaKey ||
    event.ctrlKey ||
    event.shiftKey ||
    event.altKey
  ) {
    return;
  }
  const target = event.target;
  const anchor = target instanceof Element ? target.closest('a') : null;
  if (!anchor || anchor.target || anchor.hasAttribute('download')) {
    return;
  }
  const url = new URL(anchor.href, location.href);
  if (url.origin !== location.origin || url.pathname.startsWith('/api/')) {
    return;
  }
  // In-page anchors (such as the skip link) are left to the browser.
  if (url.hash && url.pathname + url.search === location.pathname + location.search) {
    return;
  }
  event.preventDefault();
  if (url.pathname + url.search !== location.pathname + location.search) {
    navigate(url.pathname + url.search + url.hash);
  }
}

/** Starts listening for navigations; returns a function that stops it. */
export function startRouter(onChange: Listener): () => void {
  listeners.add(onChange);
  document.addEventListener('click', onClick);
  window.addEventListener('popstate', onChange);
  return () => {
    listeners.delete(onChange);
    document.removeEventListener('click', onClick);
    window.removeEventListener('popstate', onChange);
  };
}
