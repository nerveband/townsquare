<script>
  import { app, tz, today, filtered, newPostAt } from './state.svelte.js'
  import { addDays, mondayOf, dayKey, DOWS } from './time.js'
  import { canDrop, dropOnDay } from './dnd.js'
  import Card from './Card.svelte'

  const start = $derived(mondayOf(app.anchor))
  const days = $derived(Array.from({ length: 7 }, (_, i) => addDays(start, i)))
  const byDay = $derived.by(() => {
    const m = {}
    for (const s of filtered()) (m[dayKey(s.at, tz())] ||= []).push(s)
    return m
  })
  let over = $state('')
</script>

<div class="week">
  {#each days as d, i (d)}
    {@const items = byDay[d] || []}
    <section
      class="col"
      class:today={d === today()}
      class:pastempty={d < today() && !items.length}
      class:wk={i > 4}
      class:over={over === d}
      ondragover={(e) => { if (canDrop(e)) { e.preventDefault(); e.dataTransfer.dropEffect = e.altKey ? 'copy' : 'move'; over = d } }}
      ondragleave={(e) => { if (!e.currentTarget.contains(e.relatedTarget)) over = '' }}
      ondrop={(e) => { e.preventDefault(); over = ''; dropOnDay(e, d) }}
      aria-label={d}
    >
      <header><b>{+d.slice(8)}</b><span>{DOWS[i]}</span>{#if d === today()}<em>Today</em>{/if}{#if items.length}<i>{items.length}</i>{/if}</header>
      <div class="body" role="presentation" onclick={(e) => { if (e.target === e.currentTarget || e.target.classList.contains('empty')) newPostAt(d) }}>
        {#each items as s (s.schedule_id + s.occ)}
          <Card {s} />
        {:else}
          <div class="empty">{d < today() ? '' : '+ Click to add, or drop a post'}</div>
        {/each}
      </div>
    </section>
  {/each}
</div>

<style>
  .week { display: grid; grid-template-columns: repeat(7, minmax(0, 1fr)); height: 100%; min-height: 0 }
  .col { display: flex; flex-direction: column; min-width: 0; min-height: 0; border-right: 1px solid var(--line2) }
  .col:last-child { border-right: 0 }
  .wk { background: #FCFBF9 }
  header { display: flex; align-items: baseline; gap: 6px; padding: 8px 8px 6px; border-bottom: 1px solid var(--line2) }
  header b { font: 600 22px/1 var(--display) }
  header span { font: 500 10px var(--mono); letter-spacing: .1em; text-transform: uppercase; color: var(--muted) }
  header i { margin-left: auto; font: 400 10px var(--mono); font-style: normal; color: #A6ADB0 }
  .col.today { background: var(--sun-soft); box-shadow: inset 2px 0 0 var(--sun-line), inset -2px 0 0 var(--sun-line) }
  .today header { background: var(--sun); border-bottom-color: var(--sun-line) }
  .today header b, .today header span { color: var(--t900) }
  header em { font: 600 10px var(--mono); font-style: normal; letter-spacing: .08em; text-transform: uppercase; background: var(--t900); color: #fff; border-radius: 4px; padding: 2px 5px }
  .body { flex: 1; overflow: auto; padding: 6px; display: flex; flex-direction: column; gap: 6px; transition: background .12s; cursor: copy }
  .body:hover .empty { border-color: var(--t600); color: var(--t600) }
  @media (max-width: 1100px) { .week { grid-template-columns: repeat(7, minmax(170px, 1fr)); overflow-x: auto } }
  @media (max-width: 700px) {
    .week { grid-template-columns: 1fr; overflow: auto; height: 100% }
    .col { border-right: 0; border-bottom: 1px solid var(--line); min-height: auto }
    .col.pastempty { display: none }
    header { position: sticky; top: 0; background: var(--surface); z-index: 1 }
    .today header { background: var(--sun) }
    .body { overflow: visible; display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); align-items: start }
  }
  .over .body { background: var(--sky-soft); box-shadow: inset 0 0 0 2px var(--sky) }
  .empty { font-size: 11.5px; color: #B0B6B9; text-align: center; padding: 14px 4px; border: 1px dashed var(--line); border-radius: 8px }
  .empty:empty { display: none }
</style>
