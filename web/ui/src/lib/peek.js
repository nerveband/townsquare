import { app } from './state.svelte.js'

let showT, hideT
export function openPeek(s, el) {
  clearTimeout(hideT)
  clearTimeout(showT)
  showT = setTimeout(() => { app.peek = { s, rect: el.getBoundingClientRect() } }, app.peek ? 80 : 380)
}
export function closePeek() {
  clearTimeout(showT)
  hideT = setTimeout(() => { if (!app.peekPinned) app.peek = null }, 220)
}
export function holdPeek() { clearTimeout(hideT) }

/** A send that already went out (or was deleted afterwards). */
export const wentOut = (s) => ['sent', 'partial', 'unsent'].includes(s.delivery)

/** Open a send: the sent-post view if it went out, else the composer. */
export function openSend(s, scope) {
  clearTimeout(showT)
  app.peek = null
  const post = app.posts[s.post_id]
  if (!post) return
  if (wentOut(s) && !scope) { app.sent = s; return }
  app.composer = { post: JSON.parse(JSON.stringify(post)), occ: s, scope: scope || (s.repeating ? 'one' : 'all') }
}

/** Click/tap on a send: desktop opens the editor, touch devices get the preview sheet. */
export function tapSend(s, el) {
  if (matchMedia('(hover: none)').matches) {
    clearTimeout(showT)
    app.peek = { s, rect: el.getBoundingClientRect(), sheet: true }
  } else openSend(s)
}
