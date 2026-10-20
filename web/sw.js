// Service worker: only there so the installed app shows a friendly
// "Tidak ada koneksi" screen instead of the browser's error page.
//
// Everything is network-first, so a deploy is picked up on the next load
// and old JavaScript never meets a new server. The cache is only a
// fallback for being offline. Game sockets, the API and sign-in are never
// touched.
const CACHE = 'ut-shell-v1';
const SHELL = [
  '/offline.html',
  '/style.css',
  '/vendor/fonts/baloo-2-latin-400-normal.woff2',
  '/vendor/fonts/baloo-2-latin-600-normal.woff2',
  '/vendor/fonts/baloo-2-latin-800-normal.woff2',
  '/icons/icon-192.png',
];

self.addEventListener('install', (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener('activate', (e) => {
  e.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  );
});

self.addEventListener('fetch', (e) => {
  const req = e.request;
  const url = new URL(req.url);
  if (req.method !== 'GET' || url.origin !== location.origin || url.pathname.startsWith('/api/') || url.pathname.startsWith('/ws') || url.pathname.startsWith('/auth/')) {
    return;
  }
  if (req.mode === 'navigate') {
    e.respondWith(fetch(req).catch(() => caches.match('/offline.html')));
    return;
  }
  // Assets: network first, falling back to whatever we cached.
  e.respondWith(
    fetch(req)
      .then((res) => {
        if (res.ok && SHELL.includes(url.pathname)) {
          const copy = res.clone();
          caches.open(CACHE).then((c) => c.put(req, copy));
        }
        return res;
      })
      .catch(() => caches.match(req).then((hit) => hit || Response.error())),
  );
});
