<script>
  import { waPlain } from './wafmt.js'
  import { app, tz, filtered, tagColor, today } from './state.svelte.js'
  import { dayKey, prettyDay, time12, ruleLabel } from './time.js'
  import { openPeek, closePeek, openSend, tapSend } from './peek.js'
  import Avatars from './Avatars.svelte'

  const groups = $derived.by(() => {
    const out = []
    let cur = null
    for (const s of filtered().filter((x) => dayKey(x.at, tz()) >= app.anchor)) {
      const k = dayKey(s.at, tz())
      if (!cur || cur.k !== k) out.push((cur = { k, items: [] }))
      cur.items.push(s)
    }
    return out
  })
  const clean = (t) => waPlain(t)
  const nowD = new Date()
  const status = (s) => s.delivery || (app.posts[s.post_id]?.status === 'paused' ? 'paused' : 'scheduled')
</script>

<div class="list">
  {#each groups as g (g.k)}
    <div class="day" class:today={g.k === today()}>{g.k === today() ? 'Today · ' : ''}{prettyDay(g.k)}</div>
    {#each g.items as s, i (s.schedule_id + s.occ)}
      {#if g.k === today() && new Date(s.at) > nowD && (i === 0 || new Date(g.items[i - 1].at) <= nowD)}<div class="nowline"><span>Now · {time12(nowD, tz())}</span></div>{/if}
      {@const p = app.posts[s.post_id]}
      <div class="row" role="button" tabindex="0" onmouseenter={(e) => openPeek(s, e.currentTarget)} onmouseleave={closePeek} onclick={(e) => tapSend(s, e.currentTarget)} onkeydown={(e) => e.key === 'Enter' && openSend(s)}>
        <span class="tm">{time12(s.at, tz())}</span>
        <span class="sw" style="background:{tagColor(p)}"></span>
        <span class="tt"><b>{p?.title || clean(s.caption).split('\n')[0]}</b><i>{clean(s.caption)}</i></span>
        <span class="tg"><Avatars jids={s.targets} max={3} /> {s.targets.length} {s.targets.length === 1 ? 'target' : 'targets'}</span>
        <span class="rp">{s.repeating ? '↻ ' + ruleLabel(s.rrule) : 'Once'}{s.edited ? ' ✎' : ''}</span>
        <span class="st {status(s)}">{status(s)}</span>
      </div>
    {/each}
  {:else}
    <p class="none">Nothing scheduled from {prettyDay(app.anchor)} for the next 60 days.</p>
  {/each}
</div>

<style>
  .list { height: 100%; overflow: auto }
  .day { position: sticky; top: 0; background: #FBFAF8; font: 500 10.5px/1 var(--mono); letter-spacing: .1em; text-transform: uppercase; color: var(--muted); padding: 8px 12px; border-bottom: 1px solid var(--line2); z-index: 1 }
  .day.today { background: var(--sun); color: var(--t900); font-weight: 600 }
  .nowline { position: relative; height: 0; border-top: 2px solid var(--rose); margin: 6px 0 }
  .nowline span { position: absolute; left: 12px; top: -9px; background: var(--rose); color: #fff; font: 600 10px var(--mono); padding: 1px 6px; border-radius: 4px }
  .row { display: grid; grid-template-columns: 74px 8px minmax(160px, 1.6fr) minmax(130px, 1fr) 120px 84px; align-items: center; gap: 10px; height: 34px; padding: 0 12px; border-bottom: 1px solid var(--line2); font-size: 12.5px; cursor: pointer }
  .row:hover { background: #FAF9F6 }
  .tm { font: 500 11px var(--mono); color: var(--ink2) }
  .sw { width: 8px; height: 8px; border-radius: 2px }
  .tt { white-space: nowrap; overflow: hidden; text-overflow: ellipsis }
  .tt b { font-weight: 500 } .tt i { font-style: normal; color: var(--muted); margin-left: 6px }
  .tg { display: flex; align-items: center; gap: 6px; color: var(--ink2); white-space: nowrap; overflow: hidden }
  .rp { color: var(--muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis }
  .st { font-size: 11px; font-weight: 500; padding: 2px 7px; border-radius: 5px; justify-self: start; text-transform: capitalize; background: #E6F4EC; color: #11663A }
  .st.sent { background: #EEF0F1; color: #4A5960 } .st.failed, .st.missed { background: #FDE4E7; color: #9B1C2C } .st.paused, .st.blocked, .st.partial { background: #FBF0D9; color: #865A07 }
  .none { color: var(--muted); padding: 24px }
  @media (max-width: 700px) {
    .row { grid-template-columns: 62px 8px minmax(0, 1fr) 70px; height: auto; min-height: 44px; padding: 6px 12px }
    .tg, .rp { display: none }
    .tt i { display: none }
  }
</style>
