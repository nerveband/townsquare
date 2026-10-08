<script>
  import { waPlain } from './wafmt.js'
  import { onMount } from 'svelte'
  import { app, tz, today, filtered, tagColor, newPostAt } from './state.svelte.js'
  import { addDays, mondayOf, dayKey, hhmm, time12, DOWS } from './time.js'
  import { canDrop, dropOnDay, dragSend } from './dnd.js'
  import { openPeek, closePeek, openSend, tapSend } from './peek.js'

  const H = 52 // px per hour
  const start = $derived(mondayOf(app.anchor))
  const days = $derived(Array.from({ length: 7 }, (_, i) => addDays(start, i)))
  const mins = (hm) => { const [h, m] = hm.split(':').map(Number); return h * 60 + m }
  const fmtHM = (m) => `${String(Math.floor(m / 60)).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`

  // Bucket sends by day and give overlapping ones side-by-side lanes.
  const byDay = $derived.by(() => {
    const out = {}
    for (const s of filtered()) (out[dayKey(s.at, tz())] ||= []).push({ s, m: mins(hhmm(s.at, tz())) })
    for (const k in out) {
      const lanes = []
      for (const it of out[k].sort((a, b) => a.m - b.m)) {
        let l = lanes.findIndex((end) => end <= it.m)
        if (l < 0) { l = lanes.length; lanes.push(0) }
        lanes[l] = it.m + 40
        it.lane = l
      }
      for (const it of out[k]) it.lanes = lanes.length
    }
    return out
  })

  let scroller = $state()
  let over = $state('')
  let ghost = $state(null) // { day, m } hover hint
  let now = $state(Date.now())
  onMount(() => {
    scroller.scrollTop = 7 * H - 10
    const t = setInterval(() => (now = Date.now()), 60000)
    return () => clearInterval(t)
  })

  function minuteAt(e) {
    const r = e.currentTarget.getBoundingClientRect()
    const m = Math.round(((e.clientY - r.top) / H) * 60 / 15) * 15
    return Math.max(0, Math.min(23 * 60 + 45, m))
  }
  const quiet = $derived.by(() => {
    const qs = app.settings.quiet_start, qe = app.settings.quiet_end
    if (!qs || !qe || qs === qe) return []
    const a = mins(qs), b = mins(qe)
    return a < b ? [[a, b]] : [[0, b], [a, 1440]]
  })
  const clean = (t) => waPlain(t)
</script>

<div class="tg">
  <div class="head">
    <div class="gut"></div>
    {#each days as d, i (d)}<div class="dh" class:today={d === today()}><b>{+d.slice(8)}</b><span>{DOWS[i]}</span>{#if d === today()}<em>Today</em>{/if}</div>{/each}
  </div>
  <div class="scroll" bind:this={scroller}>
    <div class="grid" style="height:{24 * H}px">
      <div class="gut">
        {#each Array(24) as _, h}<span style="top:{h * H}px">{h === 0 ? '' : time12(new Date(Date.UTC(2000, 0, 1, h)), 'UTC', true)}</span>{/each}
      </div>
      {#each days as d (d)}
        <div
          class="col"
          class:today={d === today()}
          class:over={over === d}
          role="presentation"
          onclick={(e) => { if (e.target === e.currentTarget) newPostAt(d, fmtHM(minuteAt(e))) }}
          onmousemove={(e) => { if (e.target === e.currentTarget) ghost = { d, m: minuteAt(e) }; else ghost = null }}
          onmouseleave={() => (ghost = null)}
          ondragover={(e) => { if (canDrop(e)) { e.preventDefault(); over = d; ghost = { d, m: minuteAt(e) } } }}
          ondragleave={(e) => { if (!e.currentTarget.contains(e.relatedTarget)) { over = ''; ghost = null } }}
          ondrop={(e) => { e.preventDefault(); const m = minuteAt(e); over = ''; ghost = null; dropOnDay(e, d, fmtHM(m)) }}
        >
          {#each quiet as [a, b]}<div class="quiet" style="top:{(a / 60) * H}px;height:{((b - a) / 60) * H}px"></div>{/each}
          {#if d === dayKey(now, tz())}<div class="now" style="top:{(mins(hhmm(now, tz())) / 60) * H}px"><span>{time12(now, tz(), true)}</span></div>
          {:else if days.includes(dayKey(now, tz()))}<div class="now faint" style="top:{(mins(hhmm(now, tz())) / 60) * H}px"></div>{/if}
          {#if ghost && ghost.d === d}<div class="ghost" style="top:{(ghost.m / 60) * H}px">{time12(new Date(Date.UTC(2000, 0, 1, 0, ghost.m)), 'UTC', true)} {over ? '' : '+ New post'}</div>{/if}
          {#each byDay[d] || [] as it (it.s.schedule_id + it.s.occ)}
            {@const s = it.s}
            {@const p = app.posts[s.post_id]}
            <div
              class="ev"
              class:sent={s.delivery === 'sent'}
              class:fail={s.delivery === 'failed'}
              style="--c:{tagColor(p)};top:{(it.m / 60) * H}px;left:calc({(it.lane / it.lanes) * 100}% + 2px);width:calc({100 / it.lanes}% - 4px)"
              draggable="true"
              role="button"
              tabindex="0"
              ondragstart={(e) => dragSend(e, s)}
              onmouseenter={(e) => openPeek(s, e.currentTarget)}
              onmouseleave={closePeek}
              onclick={(e) => tapSend(s, e.currentTarget)}
              onkeydown={(e) => e.key === 'Enter' && openSend(s)}
            >
              {#if s.media.length && ['image', 'video'].includes(app.media[s.media[0]]?.kind)}<img src="/api/media/{s.media[0]}/preview" alt="" onerror={(e) => e.currentTarget.classList.add('broken')} />{/if}
              <span class="tx"><i>{time12(s.at, tz(), true)}{s.repeating ? ' ↻' : ''}</i><b>{p?.title || clean(s.caption).split('\n')[0]}</b></span>
            </div>
          {/each}
        </div>
      {/each}
    </div>
  </div>
</div>

<style>
  .tg { display: flex; flex-direction: column; height: 100%; min-width: 0; overflow-x: auto }
  .head, .grid { display: grid; grid-template-columns: 52px repeat(7, minmax(110px, 1fr)); min-width: 820px }
  .head { border-bottom: 1px solid var(--line2); flex: none }
  .dh { display: flex; align-items: baseline; gap: 6px; padding: 8px 8px 6px; border-left: 1px solid var(--line2) }
  .dh b { font: 600 22px/1 var(--display) }
  .dh span { font: 500 10px var(--mono); letter-spacing: .1em; text-transform: uppercase; color: var(--muted) }
  .dh.today { background: var(--sun) } .dh.today b, .dh.today span { color: var(--t900) }
  .dh em { font: 600 10px var(--mono); font-style: normal; letter-spacing: .08em; text-transform: uppercase; background: var(--t900); color: #fff; border-radius: 4px; padding: 2px 5px }
  .col.today { background: var(--sun-soft); box-shadow: inset 2px 0 0 var(--sun-line), inset -2px 0 0 var(--sun-line) }
  .scroll { flex: 1; overflow-y: auto; overflow-x: visible; min-width: 820px }
  .grid { position: relative; background-image: repeating-linear-gradient(to bottom, var(--line2) 0 1px, transparent 1px 52px) }
  .gut { position: relative }
  .grid > .gut, .head > .gut { position: sticky; left: 0; z-index: 4; background: var(--surface) }
  .gut span { position: absolute; right: 6px; transform: translateY(-50%); font: 500 10px var(--mono); color: var(--muted) }
  .col { position: relative; border-left: 1px solid var(--line2); cursor: copy }
  .col.over { background: rgba(37, 211, 102, .06) }
  .quiet { position: absolute; left: 0; right: 0; background: repeating-linear-gradient(135deg, rgba(102,119,129,.05) 0 6px, transparent 6px 12px); pointer-events: none }
  .now { position: absolute; left: 0; right: 0; height: 2px; background: var(--rose); pointer-events: none; z-index: 2 }
  .now::before { content: ""; position: absolute; left: -4px; top: -3px; width: 8px; height: 8px; border-radius: 50%; background: var(--rose) }
  .now span { position: absolute; right: 2px; top: -16px; font: 600 10px var(--mono); color: #fff; background: var(--rose); padding: 0 4px; border-radius: 3px }
  .now.faint { height: 1px; background: rgba(224, 71, 91, .35) } .now.faint::before { display: none }
  .ghost { position: absolute; left: 2px; right: 2px; height: 22px; border: 1.5px dashed var(--t600); border-radius: 6px; font: 500 10.5px var(--mono); color: var(--t600); padding: 3px 6px; pointer-events: none; background: rgba(255,255,255,.7) }
  .ev { position: absolute; height: 40px; border-radius: 6px; background: color-mix(in srgb, var(--c) 13%, #fff); border-left: 3px solid var(--c); display: flex; gap: 5px; align-items: center; padding: 3px 5px; cursor: grab; overflow: hidden; z-index: 1; box-shadow: 0 1px 2px rgba(0,0,0,.06) }
  .ev:hover { z-index: 3; box-shadow: 0 6px 16px -6px rgba(17,27,33,.3) }
  .ev img { width: 32px; height: 32px; border-radius: 4px; object-fit: cover; flex: none }
  .tx { min-width: 0; display: flex; flex-direction: column; line-height: 1.2 }
  .tx i { font: 500 10px var(--mono); font-style: normal; color: var(--ink2) }
  .tx b { font-size: 11.5px; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis }
  .sent { opacity: .55 } .fail { border-left-color: var(--rose); background: #FDECEE }
</style>
