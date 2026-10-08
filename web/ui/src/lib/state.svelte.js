import { addDays, addMonths, dayKey, mondayOf, monthStart } from './time.js'

export const app = $state({
  ready: false,
  settings: { timezone: 'America/New_York' },
  tags: [], clients: [], sets: [], targets: [], drafts: [],
  connected: false, phone: '', sending: '', sentToday: 0,
  undo: '', redo: '',
  sends: [], posts: {}, media: {}, badges: {},
  history: [],
  view: localStorage.getItem('townsquare.view') || 'week',
  anchor: '',
  hiddenTags: [], hiddenClients: [], client: '', target: '',
  showSearch: false, searchQuery: '', needSignIn: false,
  showHistory: false, showSettings: false, showTray: false,
  viewTZ: '', // preview zone; empty = default
  composer: null, // { post, occ?, scope }
  sent: null, // a send that went out, open in the sent-post view
  showDrafts: false,
  peek: null, peekPinned: false,
  toast: null,
  error: '',
  versionChanged: '',
})

/** The zone everything is shown in: a temporary preview zone, or the default from Settings. */
export const tz = () => app.viewTZ || app.settings.timezone || 'America/New_York'
export const defaultTZ = () => app.settings.timezone || 'America/New_York'
export const today = () => dayKey(new Date(), tz())

export async function api(method, path, body) {
  const opt = { method, headers: {} }
  if (body instanceof FormData) opt.body = body
  else if (body !== undefined) {
    opt.body = JSON.stringify(body)
    opt.headers['Content-Type'] = 'application/json'
  }
  const res = await fetch(path, opt)
  const text = await res.text()
  let data = null
  try { data = text ? JSON.parse(text) : null } catch { data = text }
  if (res.status === 401 && data && data.code === 'signin_required') app.needSignIn = true
  if (!res.ok) throw new Error((data && data.error) || res.statusText)
  return data
}

export async function loadState() {
  const s = await api('GET', '/api/state')
  app.settings = s.settings
  app.tags = s.tags
  app.clients = s.clients
  app.sets = s.sets
  app.drafts = s.drafts
  app.connected = s.connected
  app.phone = s.phone
  app.sending = s.sending
  app.sentToday = s.sent_today
  app.undo = s.undo
  app.redo = s.redo
  if (app.version && s.version && s.version !== app.version) app.versionChanged = s.version // restarted into an update
  app.version = s.version
  app.demo = !!s.demo
  app.telegram = s.telegram || 'off'
  app.telegramUser = s.telegram_user || ''
  if (!app.anchor) app.anchor = today()
  app.ready = true
}

export async function loadTargets() {
  app.targets = await api('GET', '/api/targets')
}

export function visibleRange() {
  if (app.view === 'week' || app.view === 'time') {
    const a = mondayOf(app.anchor)
    return [a, addDays(a, 7)]
  }
  if (app.view === 'month') {
    const a = mondayOf(monthStart(app.anchor))
    return [a, addDays(a, 42)]
  }
  return [app.anchor, addDays(app.anchor, 60)]
}

export async function loadSends() {
  if (!app.anchor) return
  const [a, b] = visibleRange()
  // pad a day each side; the UI buckets by display-time-zone day
  const r = await api('GET', `/api/sends?from=${addDays(a, -1)}&to=${addDays(b, 1)}`)
  app.sends = r.sends
  app.posts = r.posts
  app.media = r.media
  api('GET', `/api/stats/badges?from=${addDays(a, -1)}&to=${addDays(b, 1)}`).then((x) => (app.badges = x || {})).catch(() => {})
}

export async function loadHistory() {
  app.history = await api('GET', '/api/changes')
}

export async function refresh() {
  await loadState()
  await Promise.all([loadSends(), app.showHistory ? loadHistory() : null])
}

const cap = (s) => s.charAt(0).toUpperCase() + s.slice(1)
let toastTimer
export function toast(msg, actions = [], ms = 8000) {
  clearTimeout(toastTimer)
  app.toast = { msg, actions }
  toastTimer = setTimeout(() => (app.toast = null), ms)
}

/** Run a mutation, refresh, and offer Undo. */
export async function act(promise, msg, extra = []) {
  try {
    const r = await promise
    await refresh()
    toast(cap(msg || r?.summary || r?.undo || 'Saved'), [...extra, { label: 'Undo', primary: true, run: undo }])
    return r
  } catch (e) {
    toast(e.message, [], 6000)
    throw e
  }
}

export async function undo() {
  try {
    const r = await api('POST', '/api/undo')
    await refresh()
    toast(r.summary, [{ label: 'Redo', run: redo }])
  } catch (e) { toast(e.message, [], 3000) }
}

export async function redo() {
  try {
    const r = await api('POST', '/api/redo')
    await refresh()
    toast(r.summary, [{ label: 'Undo', primary: true, run: undo }])
  } catch (e) { toast(e.message, [], 3000) }
}

/** Open the composer for a new post at a day (and optional "HH:MM"). */
export function newPostAt(day, time) {
  let t = time
  if (!t) {
    const now = new Date()
    if (day === today()) {
      const p = new Intl.DateTimeFormat('en-US', { timeZone: tz(), hour: '2-digit', hourCycle: 'h23' }).format(now)
      t = String(Math.min(23, +p + 1)).padStart(2, '0') + ':00'
    } else t = '09:00'
  }
  app.peek = null
  app.composer = { post: { title: '', caption: '', media: [], targets: [], schedules: [{ start: `${day}T${t}`, tz: tz(), rrule: '' }] }, scope: 'all' }
}

export function setView(v) {
  app.view = v
  localStorage.setItem('townsquare.view', v)
  loadSends()
}

export function shift(dir) {
  if (app.view === 'month') app.anchor = addMonths(app.anchor, dir)
  else if (app.view === 'week' || app.view === 'time') app.anchor = addDays(app.anchor, 7 * dir)
  else app.anchor = addDays(app.anchor, 30 * dir)
  loadSends()
}

export function goToday() {
  app.anchor = today()
  loadSends()
}

// ---- lookups ----
export function target(jid) {
  return app.targets.find(t => t.jid === jid) || { jid, name: jid.split('@')[0], kind: 'unknown' }
}
export function tagOf(id) { return app.tags.find(t => t.id === id) }
export function clientOf(id) { return app.clients.find(c => c.id === id) }
export function tagColor(post) { return tagOf(post?.tag_id)?.color || '#9AA5A1' }

const PALETTE = ['#1673E6', '#0E8C8C', '#7C6BF0', '#C2557A', '#D9962B', '#0E2A47', '#2E9A4F', '#8A6A3B']
export function avatar(jid) {
  const t = target(jid)
  let h = 0
  for (const c of jid) h = (h * 31 + c.charCodeAt(0)) | 0
  const bg = t.kind === 'status' ? '#25D366' : t.kind === 'self' ? (jid.startsWith('tg') ? '#2AABEE' : '#075E54') : PALETTE[Math.abs(h) % PALETTE.length]
  const words = (t.name || '?').replace(/[^\p{L}\p{N} ]/gu, '').trim().split(/\s+/)
  const ini = t.kind === 'self' ? 'Me' : t.kind === 'status' ? '◌' : ((words[0]?.[0] || '?') + (words[1]?.[0] || '')).toUpperCase()
  return { bg, ini, name: t.name, kind: t.kind }
}

export const KIND_LABEL = { self: 'You', status: 'Status', announce: 'Announcements', community: 'Community', group: 'Group', channel: 'Channel', topic: 'Topic', story: 'Story' }
export const platformOf = (jid) => (!jid ? 'whatsapp' : jid.startsWith('tgbot:') ? 'telegram_bot' : jid.startsWith('tg:') ? 'telegram' : 'whatsapp')

/** Sends visible after filters. */
export function filtered() {
  return app.sends.filter(s => {
    const p = app.posts[s.post_id]
    if (!p) return false
    if (p.tag_id && app.hiddenTags.includes(p.tag_id)) return false
    if (!p.tag_id && app.hiddenTags.includes(0)) return false
    if (app.hiddenClients.includes(p.client_id || 0)) return false
    if (app.target && !s.targets.includes(app.target)) return false
    return true
  })
}

/** Quiet hours for a chat: its client's, else the post's client's, else global. Mirrors the server. */
export function quietFor(jid, postClientId) {
  const t = app.targets.find((x) => x.jid === jid)
  if (t?.kind === 'self') return { start: '', end: '', tz: app.settings.timezone, client: '' } // no quiet hours for yourself
  for (const id of [t?.client_id, postClientId]) {
    const c = id && app.clients.find((x) => x.id === id)
    if (c && c.quiet_start) return { start: c.quiet_start, end: c.quiet_end, tz: c.timezone || app.settings.timezone, client: c.name }
  }
  return { start: app.settings.quiet_start, end: app.settings.quiet_end, tz: app.settings.timezone, client: '' }
}

// If an image fails, the session may have expired: check once and show sign-in.
let authCheck = 0
export function checkAuth() {
  if (Date.now() - authCheck < 10000 || app.demo) return
  authCheck = Date.now()
  fetch('/api/auth/status').then((r) => r.json()).then((d) => { if (!d.signed_in) app.needSignIn = true }).catch(() => {})
}
