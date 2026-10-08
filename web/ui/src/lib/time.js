// Time helpers. Everything on screen is shown in the display time zone (a setting).

const fmtCache = new Map()
function fmt(tz) {
  if (!fmtCache.has(tz)) {
    fmtCache.set(tz, new Intl.DateTimeFormat('en-US', {
      timeZone: tz, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23', weekday: 'short',
    }))
  }
  return fmtCache.get(tz)
}

export function parts(date, tz) {
  const o = {}
  for (const p of fmt(tz).formatToParts(new Date(date))) o[p.type] = p.value
  return o
}

/** "YYYY-MM-DD" of an instant in tz */
export function dayKey(date, tz) {
  const p = parts(date, tz)
  return `${p.year}-${p.month}-${p.day}`
}

/** "YYYY-MM-DDTHH:MM" of an instant in tz */
export function localStr(date, tz) {
  const p = parts(date, tz)
  return `${p.year}-${p.month}-${p.day}T${p.hour}:${p.minute}`
}

export function hhmm(date, tz) {
  const p = parts(date, tz)
  return `${p.hour}:${p.minute}`
}

export function time12(date, tz, short = false) {
  const p = parts(date, tz)
  let h = +p.hour
  const ap = h >= 12 ? (short ? 'p' : ' PM') : (short ? 'a' : ' AM')
  h = h % 12 || 12
  return `${h}${p.minute === '00' && short ? '' : ':' + p.minute}${ap}`
}

export function time12Str(hm) {
  let [h, m] = hm.split(':').map(Number)
  const ap = h >= 12 ? 'PM' : 'AM'
  h = h % 12 || 12
  return `${h}:${String(m).padStart(2, '0')} ${ap}`
}

// Date-only arithmetic on "YYYY-MM-DD" strings (no time zone involved).
export function addDays(key, n) {
  const d = new Date(key + 'T00:00:00Z')
  d.setUTCDate(d.getUTCDate() + n)
  return d.toISOString().slice(0, 10)
}
export function weekday(key) { return (new Date(key + 'T00:00:00Z').getUTCDay() + 6) % 7 } // Mon=0
export function mondayOf(key) { return addDays(key, -weekday(key)) }
export function monthStart(key) { return key.slice(0, 8) + '01' }
export function addMonths(key, n) {
  const d = new Date(key.slice(0, 8) + '01T00:00:00Z')
  d.setUTCMonth(d.getUTCMonth() + n)
  return d.toISOString().slice(0, 10)
}

const MON = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
const DOW = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']
export const DOWS = DOW
export function prettyDay(key, withDow = true) {
  const [, m, d] = key.split('-').map(Number)
  return `${withDow ? DOW[weekday(key)] + ', ' : ''}${MON[m - 1]} ${d}`
}
export function monthName(key) { return `${MON[+key.slice(5, 7) - 1]} ${key.slice(0, 4)}` }
export function rangeLabel(a, b) {
  const [, am, ad] = a.split('-').map(Number)
  const [, bm, bd] = b.split('-').map(Number)
  return am === bm ? `${MON[am - 1]} ${ad}–${bd}` : `${MON[am - 1]} ${ad} – ${MON[bm - 1]} ${bd}`
}

export function tzShort(tz) {
  try {
    return new Intl.DateTimeFormat('en-US', { timeZone: tz, timeZoneName: 'short' }).formatToParts(new Date()).find(p => p.type === 'timeZoneName').value
  } catch { return tz }
}

export function ago(unix) {
  const s = Math.max(0, Date.now() / 1000 - unix)
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.floor(s / 60)} min ago`
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`
  return new Date(unix * 1000).toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
}

// ---- repeat rules ----
const BYDAY = ['MO', 'TU', 'WE', 'TH', 'FR', 'SA', 'SU']
export { BYDAY }

/** Turn a rule into an editable shape. */
export function parseRule(rule) {
  if (!rule) return { mode: 'once', days: [] }
  const kv = Object.fromEntries(rule.split(';').map(p => p.split('=')))
  const days = kv.BYDAY ? kv.BYDAY.split(',') : []
  const interval = +(kv.INTERVAL || 1)
  const extra = Object.keys(kv).filter(k => !['FREQ', 'BYDAY', 'INTERVAL'].includes(k))
  if (extra.length || interval !== 1) return { mode: 'custom', days, raw: rule }
  if (kv.FREQ === 'DAILY' && !days.length) return { mode: 'daily', days }
  if (kv.FREQ === 'WEEKLY' && days.join() === 'MO,TU,WE,TH,FR') return { mode: 'weekdays', days }
  if (kv.FREQ === 'WEEKLY') return { mode: 'weekly', days }
  if (kv.FREQ === 'MONTHLY' && !days.length) return { mode: 'monthly', days }
  return { mode: 'custom', days, raw: rule }
}

export function buildRule(mode, days, startKey, raw) {
  switch (mode) {
    case 'daily': return 'FREQ=DAILY'
    case 'weekdays': return 'FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR'
    case 'weekly': {
      const d = days.length ? days : [BYDAY[weekday(startKey)]]
      return 'FREQ=WEEKLY;BYDAY=' + BYDAY.filter(x => d.includes(x)).join(',')
    }
    case 'monthly': return 'FREQ=MONTHLY'
    case 'custom': return (raw || '').replace(/^RRULE:/, '').trim()
    default: return ''
  }
}

const DAYNAME = { MO: 'Mon', TU: 'Tue', WE: 'Wed', TH: 'Thu', FR: 'Fri', SA: 'Sat', SU: 'Sun' }
export function ruleLabel(rule) {
  const r = parseRule(rule)
  switch (r.mode) {
    case 'once': return 'Once'
    case 'daily': return 'Every day'
    case 'weekdays': return 'Weekdays'
    case 'weekly': return r.days.length === 1 ? 'Every ' + DAYNAME[r.days[0]] : 'Weekly on ' + r.days.map(d => DAYNAME[d]).join(', ')
    case 'monthly': return 'Monthly'
    default: return 'Custom repeat'
  }
}

/** Current UTC offset of tz in minutes (e.g. -420 for PDT). */
export function offsetMin(tz, at = new Date()) {
  const p = parts(at, tz)
  const asUTC = Date.UTC(+p.year, +p.month - 1, +p.day, +p.hour % 24, +p.minute)
  return Math.round((asUTC - Math.floor(new Date(at).getTime() / 60000) * 60000) / 60000)
}

export function offsetLabel(min) {
  if (min === 0) return 'same time'
  const h = Math.floor(Math.abs(min) / 60), m = Math.abs(min) % 60
  return `${min > 0 ? '+' : '−'}${h}${m ? ':' + String(m).padStart(2, '0') : ''}h`
}

export const ZONE_NAMES = {
  'Pacific/Honolulu': 'Hawaii', 'America/Anchorage': 'Alaska', 'America/Los_Angeles': 'Pacific', 'America/Denver': 'Mountain',
  'America/Phoenix': 'Arizona', 'America/Chicago': 'Central', 'America/New_York': 'Eastern', 'America/Halifax': 'Atlantic',
  'America/Sao_Paulo': 'São Paulo', 'Europe/London': 'London', 'Europe/Berlin': 'Central Europe', 'Africa/Cairo': 'Cairo',
  'Asia/Riyadh': 'Riyadh', 'Asia/Dubai': 'Dubai', 'Asia/Karachi': 'Pakistan', 'Asia/Kolkata': 'India', 'Asia/Dhaka': 'Bangladesh',
  'Asia/Singapore': 'Singapore', 'Asia/Tokyo': 'Tokyo', 'Australia/Sydney': 'Sydney', UTC: 'UTC',
}
export const zoneName = (tz) => ZONE_NAMES[tz] || tz.split('/').pop().replace(/_/g, ' ')

/** Instant for a wall-clock "YYYY-MM-DDTHH:MM" in tz. */
export function fromLocal(str, tz) {
  const [d, t = '00:00'] = str.split('T')
  const [y, m, dd] = d.split('-').map(Number)
  const [hh, mm] = t.split(':').map(Number)
  const wall = Date.UTC(y, m - 1, dd, hh, mm)
  let at = wall - offsetMin(tz, new Date(wall)) * 60000
  at = wall - offsetMin(tz, new Date(at)) * 60000
  return new Date(at)
}
