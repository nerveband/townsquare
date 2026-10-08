import { act, api, app, tz, undo } from './state.svelte.js'
import { hhmm, prettyDay } from './time.js'

export const MIME = 'application/x-townsquare'

export function dragSend(e, s) {
  e.dataTransfer.effectAllowed = 'copyMove'
  e.dataTransfer.setData(MIME, JSON.stringify({ type: 'send', post_id: s.post_id, schedule_id: s.schedule_id, occ: s.occ, at: s.at, repeating: s.repeating }))
  app.peek = null
}

export function dragDraft(e, d) {
  e.dataTransfer.effectAllowed = 'move'
  e.dataTransfer.setData(MIME, JSON.stringify({ type: 'draft', id: d.id }))
}

export function canDrop(e) {
  return e.dataTransfer.types.includes(MIME)
}

/** Handle a drop onto a day. time is optional "HH:MM" (week/day time grids). */
export async function dropOnDay(e, day, time) {
  const raw = e.dataTransfer.getData(MIME)
  if (!raw) return
  const d = JSON.parse(raw)
  if (d.type === 'draft') {
    const p = app.drafts.find(x => x.id === d.id)
    if (!p) return
    const at = `${day}T${time || '09:00'}`
    if (!p.targets.length || (!p.caption.trim() && !p.media.length)) {
      app.composer = { post: { ...p, schedules: [{ start: at, tz: tz(), rrule: '' }] }, scope: 'all' }
      return
    }
    await act(api('PUT', `/api/posts/${p.id}`, { ...p, status: 'scheduled', schedules: [{ start: at, tz: tz(), rrule: '' }] }),
      `Scheduled ${p.title || 'draft'} for ${prettyDay(day)}, ${time || '9:00 AM'}`)
    return
  }
  const to = `${day}T${time || hhmm(d.at, tz())}`
  if (e.altKey) {
    await act(api('POST', '/api/sends/copy', { ...d, to, tz: tz() }))
    return
  }
  if (d.repeating) {
    await act(api('POST', '/api/sends/move', { ...d, to, tz: tz(), scope: 'one' }), null, [{
      label: 'Move whole series',
      run: async () => {
        await api('POST', '/api/undo')
        await act(api('POST', '/api/sends/move', { ...d, to, tz: tz(), scope: 'all' }))
      },
    }])
    return
  }
  await act(api('POST', '/api/sends/move', { ...d, to, tz: tz() }))
}
