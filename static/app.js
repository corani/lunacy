/* Lunacy SPA — vanilla JS, no framework */
'use strict';

// ── State ────────────────────────────────────────────────────────────────────
const state = {
  route: null,        // { type, param } — current view
  books: [],          // loaded book objects
  offset: 0,
  limit: 48,
  loading: false,
  allLoaded: false,
  searchTerm: '',
  sortBy: 'recent',
  sortDir: 'desc',
  detail: null,       // book currently shown in detail panel
};

// ── DOM refs ─────────────────────────────────────────────────────────────────
const $ = id => document.getElementById(id);
const grid       = $('grid');
const gridWrap   = $('grid-wrap');
const loadingEl  = $('loading');
const emptyEl    = $('empty');
const sentinel   = $('sentinel');
const searchEl   = $('search');
const sortEl     = $('sort-select');
const sortDirEl  = $('sort-dir');
const pageTitleEl = $('page-title');
const statsEl    = $('stats-display');
const detail     = $('detail');
const detailClose = $('detail-close');

// ── API ───────────────────────────────────────────────────────────────────────
async function api(path) {
  const r = await fetch(path);
  if (!r.ok) throw new Error(`${r.status} ${r.statusText} — ${path}`);
  return r.json();
}

function bookURL(filename, suffix) {
  return `/api/book${suffix || ''}?f=${encodeURIComponent(filename)}`;
}

// ── Routing ───────────────────────────────────────────────────────────────────
function parseHash(hash) {
  const h = hash.replace(/^#\//, '');
  if (!h || h === '') return { type: 'shelf', param: '' };
  const parts = h.split('/');
  switch (parts[0]) {
    case 'shelf':   return { type: 'shelf',   param: decodeURIComponent(parts[1] || '') };
    case 'authors': return parts[1]
                      ? { type: 'author-books', param: decodeURIComponent(parts[1]) }
                      : { type: 'authors', param: '' };
    case 'series':  return parts[1]
                      ? { type: 'series-books', param: decodeURIComponent(parts[1]) }
                      : { type: 'series', param: '' };
    case 'books':   return { type: 'book-detail', param: decodeURIComponent(parts[1] || '') };
    default:        return { type: 'shelf', param: '' };
  }
}

function navigate(route, keepSearch) {
  closeDetail();
  state.route     = route;
  state.books     = [];
  state.offset    = 0;
  state.allLoaded = false;
  if (!keepSearch) {
    state.searchTerm = '';
    searchEl.value  = '';
  }
  grid.innerHTML  = '';
  emptyEl.style.display = 'none';

  updateNavActive(route);

  if (route.type === 'authors') {
    pageTitleEl.textContent = 'Authors';
    loadAuthors();
  } else if (route.type === 'series') {
    pageTitleEl.textContent = 'Series';
    loadSeries();
  } else if (route.type === 'author-books') {
    pageTitleEl.textContent = route.param;
    loadBooks();
  } else if (route.type === 'series-books') {
    pageTitleEl.textContent = route.param;
    loadBooks();
  } else if (route.type === 'book-detail') {
    openDetail(route.param);
  } else {
    const label = route.param ? route.param : 'All books';
    pageTitleEl.textContent = label;
    loadBooks();
  }
}

function updateNavActive(route) {
  document.querySelectorAll('.nav-item').forEach(el => el.classList.remove('active'));
  let key = route.type === 'shelf' ? `shelf:${route.param}` : route.type;
  document.querySelectorAll(`.nav-item[data-route="${key}"]`).forEach(el => el.classList.add('active'));
}

// ── Book grid loading ─────────────────────────────────────────────────────────
function buildBookQuery(extra) {
  const params = new URLSearchParams({
    sort:   state.sortBy,
    dir:    state.sortDir,
    limit:  state.limit,
    offset: state.offset,
    ...extra,
  });
  if (state.searchTerm) params.set('search', state.searchTerm);
  if (extra.shelf !== undefined) {} // already set
  return `/api/books?${params}`;
}

function currentFilter() {
  const r = state.route;
  if (!r) return {};
  if (r.type === 'shelf')        return { shelf: r.param };
  if (r.type === 'author-books') return { author: r.param };
  if (r.type === 'series-books') return { series: r.param };
  return {};
}

async function loadBooks() {
  if (state.loading || state.allLoaded) return;
  state.loading = true;
  loadingEl.style.display = 'block';

  try {
    const query = buildBookQuery(currentFilter());
    const books = await api(query);
    state.loading = false;
    loadingEl.style.display = 'none';

    if (!books || books.length === 0) {
      if (state.offset === 0) emptyEl.style.display = 'block';
      state.allLoaded = true;
      return;
    }

    books.forEach(b => renderBookCard(b));
    state.books.push(...books);
    state.offset += books.length;
    if (books.length < state.limit) state.allLoaded = true;
  } catch (e) {
    state.loading = false;
    loadingEl.style.display = 'none';
    console.error(e);
  }
}

function renderBookCard(b) {
  const card = document.createElement('div');
  card.className = 'book-card';
  card.dataset.filename = b.filename;

  const coverWrap = document.createElement('div');
  coverWrap.className = 'book-cover';
  const img = document.createElement('img');
  img.src = bookURL(b.filename, '/cover');
  img.alt = b.title;
  img.loading = 'lazy';
  coverWrap.appendChild(img);

  // Progress bar
  if (b.pct > 0.5) {
    const bar = document.createElement('div');
    bar.className = 'book-progress';
    bar.style.width = `${Math.min(b.pct, 100)}%`;
    if (b.read_state === 3 || b.pct >= 100) bar.style.background = 'var(--progress-done)';
    coverWrap.appendChild(bar);
  }

  // Annotation badge
  if (b.annot_count > 0) {
    const badge = document.createElement('div');
    badge.className = 'badge badge-annot';
    badge.textContent = b.annot_count;
    coverWrap.appendChild(badge);
  }

  // Star rating badge
  if (b.rate > 0) {
    const badge = document.createElement('div');
    badge.className = 'badge badge-rate';
    badge.textContent = '★'.repeat(b.rate);
    coverWrap.appendChild(badge);
  }

  const title = document.createElement('div');
  title.className = 'book-title';
  title.textContent = b.title || b.filename;

  card.appendChild(coverWrap);
  card.appendChild(title);
  card.addEventListener('click', () => openDetail(b.filename));
  grid.appendChild(card);
}

// ── Author / Series grids ─────────────────────────────────────────────────────
async function loadAuthors() {
  loadingEl.style.display = 'block';
  try {
    const authors = await api('/api/authors');
    loadingEl.style.display = 'none';
    if (!authors || authors.length === 0) { emptyEl.style.display = 'block'; return; }
    authors.forEach(a => renderEntityCard(a.author, a.book_count, `#/authors/${encodeURIComponent(a.author)}`, `/api/author/cover?name=${encodeURIComponent(a.author)}`));
  } catch (e) { loadingEl.style.display = 'none'; console.error(e); }
}

async function loadSeries() {
  loadingEl.style.display = 'block';
  try {
    const series = await api('/api/series');
    loadingEl.style.display = 'none';
    if (!series || series.length === 0) { emptyEl.style.display = 'block'; return; }
    series.forEach(s => renderEntityCard(s.series, s.book_count, `#/series/${encodeURIComponent(s.series)}`, `/api/series/cover?name=${encodeURIComponent(s.series)}`));
  } catch (e) { loadingEl.style.display = 'none'; console.error(e); }
}

function renderEntityCard(name, count, href, coverSrc) {
  const card = document.createElement('div');
  card.className = 'entity-card';
  // Apply current search filter immediately.
  if (state.searchTerm && !name.toLowerCase().includes(state.searchTerm.toLowerCase())) {
    card.style.display = 'none';
  }

  const coverWrap = document.createElement('div');
  coverWrap.className = 'entity-cover';
  const img = document.createElement('img');
  img.src = coverSrc;
  img.alt = name;
  img.loading = 'lazy';
  coverWrap.appendChild(img);

  const nameEl = document.createElement('div');
  nameEl.className = 'entity-name';
  nameEl.textContent = name;

  const countEl = document.createElement('div');
  countEl.className = 'entity-count';
  countEl.textContent = `${count} book${count !== 1 ? 's' : ''}`;

  card.appendChild(coverWrap);
  card.appendChild(nameEl);
  card.appendChild(countEl);
  card.addEventListener('click', () => { window.location.hash = href; });
  grid.appendChild(card);
}

// ── Book detail ───────────────────────────────────────────────────────────────
async function openDetail(filename) {
  state.detail = filename;

  try {
    const [{ book, reading_days }, anns] = await Promise.all([
      api(bookURL(filename)),
      api(bookURL(filename, '/annotations')),
    ]);
    renderDetail(book, reading_days || [], anns || []);
    detail.classList.add('open');
  } catch (e) {
    console.error(e);
  }
}

function renderDetail(book, days, anns) {
  // Cover
  const coverEl = $('detail-cover');
  coverEl.innerHTML = '';
  const img = document.createElement('img');
  img.src = bookURL(book.filename, '/cover');
  img.alt = book.title;
  coverEl.appendChild(img);

  $('detail-title').textContent = book.title || book.filename;

  const authorEl = $('detail-author');
  if (book.author) {
    // author field may contain multiple authors separated by newlines
    const authors = book.author.split('\n').map(a => a.trim()).filter(Boolean);
    authorEl.innerHTML = authors
      .map(a => `<a href="#/authors/${encodeURIComponent(a)}">${esc(a)}</a>`)
      .join(', ');
  } else {
    authorEl.textContent = '';
  }

  const seriesEl = $('detail-series');
  if (book.series) {
    const idx = book.series_index ? ` #${book.series_index}` : '';
    seriesEl.innerHTML = `Series: <a href="#/series/${encodeURIComponent(book.series)}">${esc(book.series)}${idx}</a>`;
  } else {
    seriesEl.textContent = '';
  }

  const metaEl = $('detail-meta');
  const stars = book.rate > 0 ? `<span>${'★'.repeat(book.rate)}${'☆'.repeat(5 - book.rate)}</span>` : '';
  const shelf  = book.favorite ? `<span>${esc(book.favorite)}</span>` : '';
  const added  = book.add_time ? new Date(book.add_time).toLocaleDateString() : '';
  metaEl.innerHTML = [stars, shelf, added ? `Added <span>${added}</span>` : ''].filter(Boolean).join(' &nbsp;·&nbsp; ');

  // Progress
  const progEl = $('detail-progress');
  if (book.pct > 0) {
    const finished = book.read_state === 3 || book.pct >= 100;
    const label = finished ? 'Finished' : `${book.pct.toFixed(1)}%`;
    const color = finished ? 'var(--progress-done)' : 'var(--progress)';

    // Compute totals from reading days
    let totalMs = 0, totalWords = 0;
    for (const d of days) { totalMs += d.read_time_ms; totalWords += d.words_read; }
    const hours = totalMs / 3600000;
    const wpm = totalMs > 0 ? Math.round(totalWords / (totalMs / 60000)) : 0;

    let histHTML = '';
    if (days.length > 0) {
      const epoch = new Date(0);
      const rows = [...days].reverse().map(d => {
        const date = new Date(d.day * 86400000).toISOString().slice(0, 10);
        const h = Math.floor(d.read_time_ms / 3600000);
        const m = Math.floor((d.read_time_ms % 3600000) / 60000);
        const s = Math.floor((d.read_time_ms % 60000) / 1000);
        const dwpm = d.read_time_ms > 0 ? Math.round(d.words_read / (d.read_time_ms / 60000)) : 0;
        const time = `${h}:${String(m).padStart(2,'0')}:${String(s).padStart(2,'0')}`;
        return `<tr><td>${date}</td><td>${time}</td><td>${dwpm} w/m</td><td>${d.pct.toFixed(1)}%</td></tr>`;
      }).join('');
      histHTML = `
        <div class="reading-totals">${hours.toFixed(2)}h &nbsp;·&nbsp; ${wpm} w/m avg</div>
        <table class="reading-history"><tbody>${rows}</tbody></table>`;
    }

    progEl.innerHTML = `
      <div class="progress-bar-wrap"><div class="progress-bar-fill" style="width:${Math.min(book.pct,100)}%;background:${color}"></div></div>
      <div class="progress-label">${label}</div>
      ${histHTML}`;
  } else {
    progEl.innerHTML = '';
  }

  // Description + Annotations
  const annEl = $('detail-annotations');
  let html = '';
  if (book.description) {
    html += `<p id="detail-description">${esc(book.description)}</p>`;
  }
  if (anns.length > 0) {
    const items = anns.map(a => {
      const color = argbToCSS(a.color);
      const locationLabel = a.page != null ? `p.${a.page}` : a.chapter != null ? `ch.${a.chapter + 1}` : '';
      return `<div class="annot-item" style="border-color:${color}">
        ${a.text ? `<div class="annot-text">${esc(a.text)}</div>` : ''}
        ${a.note ? `<div class="annot-note">${esc(a.note)}</div>` : ''}
        <div class="annot-meta">${locationLabel}</div>
      </div>`;
    }).join('');
    html += `<h3>${anns.length} annotation${anns.length !== 1 ? 's' : ''}</h3>${items}`;
  }
  annEl.innerHTML = html;
}

function closeDetail() {
  detail.classList.remove('open');
  state.detail = null;
}

// ── Stats ─────────────────────────────────────────────────────────────────────
async function loadStats() {
  try {
    const s = await api('/api/stats');
    statsEl.innerHTML = `<span>${s.total_books}</span> books<br><span>${(s.total_words / 1000).toFixed(0)}k</span> words`;
  } catch {}
}

// ── Helpers ───────────────────────────────────────────────────────────────────
function esc(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

function argbToCSS(packed) {
  const u = packed >>> 0;
  const a = ((u >> 24) & 0xff) / 255;
  const r = (u >> 16) & 0xff;
  const g = (u >>  8) & 0xff;
  const b =  u        & 0xff;
  return `rgba(${r},${g},${b},${Math.max(a, 0.6).toFixed(2)})`;
}

// ── Infinite scroll ───────────────────────────────────────────────────────────
const observer = new IntersectionObserver(entries => {
  if (entries[0].isIntersecting) loadBooks();
}, { rootMargin: '200px' });
observer.observe(sentinel);

// ── Detail panel resize ───────────────────────────────────────────────────────
const detailResize = $('detail-resize');
const DETAIL_W_KEY = 'lunacy-detail-w';

// Restore saved width.
const savedW = localStorage.getItem(DETAIL_W_KEY);
if (savedW) document.documentElement.style.setProperty('--detail-w', savedW + 'px');

detailResize.addEventListener('mousedown', e => {
  e.preventDefault();
  const startX = e.clientX;
  const startW = detail.offsetWidth;
  detailResize.classList.add('dragging');
  detail.classList.add('resizing');

  function onMove(e) {
    const w = Math.max(280, Math.min(window.innerWidth * 0.8, startW - (e.clientX - startX)));
    document.documentElement.style.setProperty('--detail-w', w + 'px');
  }
  function onUp() {
    detailResize.classList.remove('dragging');
    detail.classList.remove('resizing');
    localStorage.setItem(DETAIL_W_KEY, detail.offsetWidth);
    window.removeEventListener('mousemove', onMove);
    window.removeEventListener('mouseup', onUp);
  }
  window.addEventListener('mousemove', onMove);
  window.addEventListener('mouseup', onUp);
});

// ── Event wiring ──────────────────────────────────────────────────────────────
detailClose.addEventListener('click', () => {
  closeDetail();
  if (state.route?.type === 'book-detail') history.back();
});

let searchDebounce = null;
searchEl.addEventListener('input', () => {
  state.searchTerm = searchEl.value;
  const r = state.route;
  if (!r) return;

  if (r.type === 'authors' || r.type === 'series') {
    // Client-side filter — entities are all loaded.
    const term = state.searchTerm.toLowerCase();
    document.querySelectorAll('.entity-card').forEach(card => {
      const name = card.querySelector('.entity-name')?.textContent || '';
      card.style.display = !term || name.toLowerCase().includes(term) ? '' : 'none';
    });
  } else {
    // Server-side search — debounce and reload.
    clearTimeout(searchDebounce);
    searchDebounce = setTimeout(() => navigate(r, true), 300);
  }
});

sortEl.addEventListener('change', () => {
  state.sortBy = sortEl.value;
  const r = state.route;
  if (r) navigate(r, true);
});

sortDirEl.addEventListener('click', () => {
  state.sortDir = state.sortDir === 'desc' ? 'asc' : 'desc';
  sortDirEl.textContent = state.sortDir === 'desc' ? '↓' : '↑';
  const r = state.route;
  if (r) navigate(r, true);
});

document.querySelectorAll('.nav-item[data-route]').forEach(el => {
  el.addEventListener('click', e => {
    e.preventDefault();
    window.location.hash = el.getAttribute('href');
  });
});

// ── Hash router ───────────────────────────────────────────────────────────────
window.addEventListener('hashchange', () => {
  const route = parseHash(window.location.hash);
  navigate(route);
});

// ── Boot ──────────────────────────────────────────────────────────────────────
loadStats();
const initRoute = parseHash(window.location.hash);
navigate(initRoute.type === 'book-detail'
  ? { type: 'shelf', param: '' }  // go to grid first, then open detail
  : initRoute);
if (initRoute.type === 'book-detail') openDetail(initRoute.param);
