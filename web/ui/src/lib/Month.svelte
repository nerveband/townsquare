<script>
  import { waPlain } from './wafmt.js'
  import { app, tz, today, filtered, tagColor, newPostAt } from './state.svelte.js'
  import { addDays, mondayOf, monthStart, dayKey, DOWS, time12 } from './time.js'
  import { canDrop, dropOnDay, dragSend } from './dnd.js'
  import { openPeek, closePeek, openSend, tapSend } from './peek.js'

  const start = $derived(mondayOf(monthStart(app.anchor)))
  const days = $derived(Array.from({ length: 42 }, (_, i) => addDays(start, i)))
  const month = $derived(app.anchor.slice(0, 7))
  const byDay = $derived.by(() => {
    const m = {}
    for (const s of filtered()) (m[dayKey(s.at, tz())] ||= []).push(s)
    return m
  })
  let over = $state('')
  const MAX = 4
  const clean = (t) => waPlain(t)
  function openWeek(d) { app.anchor = d; app.view = 'week'; localStorage.setItem('townsquare.view', 'week') }
</script>

<div class="month">
  <div class="dows">{#each DOWS as d}<div>{d}</div>{/each}</div>
  <div class="grid">
    {#each days as d (d)}
      {@const items = byDay[d] || []}
      {@const show = items.length > MAX ? MAX - 1 : MAX}
      <div
        class="day"
        class:out={d.slice(0, 7) !== month}
        class:today={d === today()}
        class:over={over === d}
        role="gridcell"
        tabindex="-1"
        ondragover={(e) => { if (canDrop(e)) { e.preventDefault(); over = d } }}
        ondragleave={(e) => { if (!e.currentTarget.contains(e.relatedTarget)) over = '' }}
        ondrop={(e) => { e.preventDefault(); over = ''; dropOnDay(e, d) }}
        onclick={(e) => { if (e.target === e.currentTarget || e.target.closest('.dn')) { if (matchMedia('(max-width: 700px)').matches) openWeek(d); else newPostAt(d) } }}
        onkeydown={() => {}}
      >
        <div class="dn"><span>{+d.slice(8)}</span>{#if items.length > 2}<i>{items.length}</i>{/if}</div>
        {#each items.slice(0, show) as s (s.schedule_id + s.occ)}
          {@const p = app.posts[s.post_id]}
          <div
            class="ln"
            class:sent={s.delivery === 'sent'}
            class:fail={s.delivery === 'failed'}
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
            {#if s.media.length && (app.media[s.media[0]]?.kind === 'image' || app.media[s.media[0]]?.kind === 'video')}
              <img src="/api/media/{s.media[0]}/preview" alt="" onerror={(e) => e.currentTarget.classList.add('broken')} />
            {:else}
              <span class="sq"></span>
            {/if}
            <span class="tm">{time12(s.at, tz(), true)}</span>
            <span class="tt">{p?.title || clean(s.caption).split('\n')[0]}</span>
            {#if s.repeating}<span class="rp">↻</span>{/if}
          </div>
        {/each}
        {#if items.length > show}<button class="more" onclick={() => openWeek(d)}>+{items.length - show} more</button>{/if}
      </div>
    {/each}
  </div>
</div>

<style>
  .month { display: flex; flex-direction: column; height: 100% }
  .dows { display: grid; grid-template-columns: repeat(7, minmax(0, 1fr)); border-bottom: 1px solid var(--line) }
  .dows div { font: 500 10px var(--mono); letter-spacing: .1em; color: var(--muted); text-transform: uppercase; padding: 7px 8px }
  .grid { display: grid; grid-template-columns: repeat(7, minmax(0, 1fr)); grid-auto-rows: minmax(0, 1fr); flex: 1; min-height: 0 }
  .day { border-right: 1px solid var(--line2); border-bottom: 1px solid var(--line2); padding: 3px; display: flex; flex-direction: column; gap: 2px; min-width: 0; min-height: 0; overflow: hidden }
  .day:nth-child(7n) { border-right: 0 }
  .day { cursor: copy }
  .day:hover { background: #FBFAF7 }
  @media (max-width: 700px) {
    .ln { height: 6px; padding: 0; border-radius: 3px; background: var(--c) }
    .ln > * { display: none }
    .day { gap: 2px; padding: 2px }
    .more { font-size: 10px; padding: 0 2px }
  }
  .dn { font-size: 11.5px; font-weight: 500; color: var(--ink2); padding: 1px 4px; display: flex; justify-content: space-between }
  .dn i { font: 400 10px var(--mono); font-style: normal; color: #A6ADB0 }
  .out .dn { color: #B9BFC2 }
  .today .dn span { background: var(--sun); color: var(--t900); border-radius: 5px; padding: 0 5px; font-weight: 600 }
  .day.today { background: var(--sun-soft); box-shadow: inset 0 0 0 2px var(--sun-line) }
  .today .dn::after { content: "Today"; font: 600 9.5px var(--mono); letter-spacing: .08em; text-transform: uppercase; color: var(--t800); margin-left: auto; margin-right: 4px }
  .today .dn i { display: none }
  .over { background: var(--sky-soft); box-shadow: inset 0 0 0 2px var(--sky) }
  .ln { height: 20px; flex: none; display: flex; align-items: center; gap: 5px; padding: 0 5px 0 3px; border-radius: 5px; font-size: 11.5px; cursor: grab; background: color-mix(in srgb, var(--c) 11%, #fff); min-width: 0 }
  .ln:hover { background: color-mix(in srgb, var(--c) 20%, #fff) }
  .ln img, .sq { width: 14px; height: 14px; border-radius: 3px; object-fit: cover; flex: none; background: var(--c) }
  .tm { font: 500 10px var(--mono); color: var(--ink2); flex: none }
  .tt { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0 }
  .rp { margin-left: auto; color: var(--muted); font-size: 10px }
  .sent { opacity: .5 }
  .fail { background: #FDECEE }
  .more { border: 0; background: transparent; text-align: left; font-size: 11px; color: var(--muted); padding: 1px 6px; border-radius: 4px }
  .more:hover { background: var(--sunk); color: var(--ink) }
</style>
