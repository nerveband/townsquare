<script>
  import { waHTML, waPlain } from './wafmt.js'
  import { app, tz, tagColor } from './state.svelte.js'
  import { time12, ruleLabel, tzShort } from './time.js'
  import { dragSend } from './dnd.js'
  import Thumb from './Thumb.svelte'
  import Avatars from './Avatars.svelte'
  import Platform from './Platform.svelte'
  import { openPeek, closePeek, openSend, tapSend } from './peek.js'

  let { s } = $props()
  const p = $derived(app.posts[s.post_id])
  const clean = (t) => (t || '').replace(/[*_~`]/g, '')
  const state = $derived(
    s.delivery === 'sent' ? 'sent' : s.delivery === 'failed' ? 'failed' : s.delivery === 'blocked' ? 'blocked' : s.delivery === 'partial' ? 'partial' : p?.status === 'paused' ? 'paused' : ''
  )
  const badge = $derived(app.badges[`${s.schedule_id}|${s.occ}`])
  const fmt = (n) => (n >= 1000 ? `${(n / 1000).toFixed(n >= 10000 ? 0 : 1)}k` : n)
  const stateLabel = { sent: 'Sent ✓', failed: 'Failed', blocked: 'Held by safe mode', partial: 'Partly sent', paused: 'Paused' }
</script>

{#if p}
  <div
    class="card {state}"
    style="--c:{tagColor(p)}"
    draggable="true"
    role="button"
    tabindex="0"
    ondragstart={(e) => dragSend(e, s)}
    onmouseenter={(e) => openPeek(s, e.currentTarget)}
    onmouseleave={closePeek}
    onclick={(e) => tapSend(s, e.currentTarget)}
    onkeydown={(e) => e.key === 'Enter' && openSend(s)}
  >
    {#if s.media.length}<div class="th"><Thumb media={s.media} caption={s.caption} /></div>{/if}
    <div class="b">
      <div class="meta">
        <span class="mono">{time12(s.at, tz(), true)}</span>{#if s.tz !== tz()}<span class="alt" title="Scheduled in {s.tz}">{time12(s.at, s.tz, true)} {tzShort(s.tz)}</span>{/if}
        {#if s.repeating}<span class="rep" title="Repeating post">↻ {ruleLabel(s.rrule)}</span>{/if}
        {#if s.edited}<span class="ed" title="This send was edited separately">✎</span>{/if}
        {#if state}<span class="st">{stateLabel[state]}</span>{/if}
      </div>
      <b>{p.title || waPlain(s.caption).split('\n')[0] || 'Untitled'}</b>
      {#if s.media.length}
        {#if s.caption}<p class="wa">{@html waHTML(s.caption.replace(/\n{2,}/g, '\n'))}</p>{/if}
      {:else}
        <p class="bubble wa">{@html waHTML(s.caption.replace(/\n{2,}/g, '\n'))}</p>
      {/if}
      {#if badge && (badge.reach || badge.reactions)}
        <div class="eng" title="{badge.reach} reached{badge.members ? ` of ${badge.members}` : ''} · {badge.reactions} reactions · {badge.replies} replies">
          <span><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"><path d="M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7S2 12 2 12z" /><circle cx="12" cy="12" r="3" /></svg>{fmt(badge.reach)}</span>
          {#if badge.reactions}<span><svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 21s-7.5-4.6-9.6-9.2C.9 8.4 3 5 6.4 5c2 0 3.6 1.1 4.4 2.6h2.4C14 6.1 15.6 5 17.6 5 21 5 23.1 8.4 21.6 11.8 19.5 16.4 12 21 12 21z" /></svg>{fmt(badge.reactions)}</span>{/if}
          {#if badge.replies}<span><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"><path d="M21 12a8 8 0 01-11.6 7.1L3 21l1.9-6.4A8 8 0 1121 12z" /></svg>{fmt(badge.replies)}</span>{/if}
        </div>
      {/if}
      <div class="foot">{#if s.targets.some((j) => j.startsWith('tg:'))}<Platform platform="telegram" />{/if}<Avatars jids={s.targets} max={4} size={16} /><span>{s.targets.length === 1 ? app.targets.find((t) => t.jid === s.targets[0])?.name || '1 target' : s.targets.length + ' targets'}</span></div>
    </div>
  </div>
{/if}

<style>
  .eng { display: flex; gap: 9px; font: 500 11px var(--mono); color: var(--t800); margin: 2px 0 4px }
  .eng span { display: inline-flex; align-items: center; gap: 3px }
  .eng svg { width: 12px; height: 12px }
  .card { border-radius: 9px; background: var(--surface); border: 1px solid var(--line); overflow: hidden; cursor: grab; flex: none; position: relative; transition: box-shadow .15s, border-color .15s }
  .card:hover { box-shadow: 0 6px 18px -8px rgba(17,27,33,.3); border-color: #D2CCC2 }
  .card::before { content: ""; position: absolute; left: 0; top: 0; right: 0; height: 2px; background: var(--c); z-index: 1 }
  .th { height: 64px; display: flex }
  .b { padding: 6px 7px 7px }
  .meta { display: flex; flex-wrap: wrap; row-gap: 3px; align-items: center; gap: 5px; font-size: 10.5px; color: var(--ink2); white-space: nowrap; min-width: 0 }
  .meta .mono { font-weight: 500 }
  .alt { font: 500 9.5px var(--mono); color: #1B6A93; background: #EEF6FD; border-radius: 4px; padding: 1px 4px }
  .rep { color: var(--t800); background: #E3F2EE; border-radius: 4px; padding: 1px 5px; overflow: hidden; text-overflow: ellipsis; min-width: 0 }
  .ed { color: #865A07 }
  .st { margin-left: auto; color: var(--muted) }
  b { display: block; font-size: 12px; font-weight: 600; margin-top: 3px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis }
  p { margin: 2px 0 5px; font-size: 11.5px; line-height: 1.35; color: var(--ink2); display: -webkit-box; -webkit-line-clamp: 2; line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
  p.bubble { -webkit-line-clamp: 4; line-clamp: 4; background: var(--note); border-radius: 6px 2px 6px 6px; padding: 5px 7px; margin: 5px 0 6px; color: var(--ink) }
  .foot { display: flex; align-items: center; gap: 5px; font-size: 10.5px; color: var(--muted); min-width: 0 }
  .foot span:last-child { white-space: nowrap; overflow: hidden; text-overflow: ellipsis }
  .sent { opacity: .6 }
  .failed { border-color: #F2B5BE } .failed .st { color: var(--rose) }
  .blocked .st { color: #865A07 }
  .paused { opacity: .75 } .paused .st { color: #865A07 }
</style>
