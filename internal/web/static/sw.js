// mend service worker — intentionally minimal.
//
// Per the design log: cache only the app shell + a static offline page. NEVER
// cache dynamic responses — stale medical data is worse than an error. So the
// only thing we serve from cache is the /offline fallback (and the static icons
// it needs); every real request, including htmx fragments, goes to the network.

const CACHE = 'mend-shell-v1';
const SHELL = [
  '/offline',
  '/manifest.webmanifest',
  '/static/icon-192.png',
  '/static/icon-512.png',
];

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting())
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (event) => {
  const req = event.request;
  if (req.method !== 'GET') return;

  // Only intercept top-level navigations. If the network is unreachable, show the
  // cached offline page. Everything else (htmx fragments, audio, APIs) is left to
  // the network untouched and is never cached.
  if (req.mode === 'navigate') {
    event.respondWith(fetch(req).catch(() => caches.match('/offline')));
  }
});
