<script>
  import { onMount } from 'svelte'
  import { app, refresh, loadTargets, loadSends, setView, shift, goToday, undo, redo, tz, defaultTZ, today, newPostAt } from './lib/state.svelte.js'
  import { mondayOf, addDays, rangeLabel, monthName, tzShort, ruleLabel } from './lib/time.js'
  import { dragDraft } from './lib/dnd.js'
  import Week from './lib/Week.svelte'
  import TimeGrid from './lib/TimeGrid.svelte'
  import Month from './lib/Month.svelte'
  import List from './lib/List.svelte'
  import Peek from './lib/Peek.svelte'
  import Composer from './lib/Composer.svelte'
  import History from './lib/History.svelte'
  import Settings from './lib/Settings.svelte'
  import Toast from './lib/Toast.svelte'
  import TZSlider from './lib/TZSlider.svelte'
  import { zoneName } from './lib/time.js'
  import CheckList from './lib/CheckList.svelte'
  import Search from './lib/Search.svelte'
  import SignIn from './lib/SignIn.svelte'
  import Stats from './lib/Stats.svelte'

  onMount(() => {
    refresh().then(loadTargets).catch((e) => (app.error = e.message))
    const t = setInterval(() => { if (!app.composer && !app.needSignIn) refresh().catch(() => {}) }, 20000)
    return () => clearInterval(t)
  })

  const label = $derived.by(() => {
    if (!app.anchor) return ''
    if (app.view === 'week' || app.view === 'time') { const a = mondayOf(app.anchor); return rangeLabel(a, addDays(a, 6)) }
    if (app.view === 'month') return monthName(app.anchor)
    if (app.view === 'stats') return 'Stats'
    return 'Upcoming'
  })

  function newPost() { newPostAt(app.anchor > today() ? app.anchor : today()) }
  function key(e) {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); app.showSearch = !app.showSearch; return }
    if (app.showSearch) return
    if (e.target.closest('input,textarea,select,[contenteditable]')) return
    if (e.key === '/' && !app.composer && !app.showSettings) { e.preventDefault(); app.showSearch = true; return }
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'z') { e.preventDefault(); e.shiftKey ? redo() : undo() }
    else if (app.composer || app.showSettings || e.metaKey || e.ctrlKey || e.altKey) return
    else if (e.key === 'n') { e.preventDefault(); newPost() }
    else if (e.key === 'w') setView('week')
    else if (e.key === 'g') setView('time')
    else if (e.key === 'm') setView('month')
    else if (e.key === 'l') setView('list')
    else if (e.key === 's') setView('stats')
    else if (e.key === 't') goToday()
    else if (e.key === 'ArrowLeft') shift(-1)
    else if (e.key === 'ArrowRight') shift(1)
    else if (e.key === 'h') app.showHistory = !app.showHistory
  }
  const clean = (t) => (t || '').replace(/[*_~`]/g, '')
  // How many sends in view use each tag / client (shown next to the checkboxes).
  const tagCounts = $derived.by(() => { const c = {}; for (const s of app.sends) { const k = app.posts[s.post_id]?.tag_id || 0; c[k] = (c[k] || 0) + 1 } return c })
  const clientCounts = $derived.by(() => { const c = {}; for (const s of app.sends) { const k = app.posts[s.post_id]?.client_id || 0; c[k] = (c[k] || 0) + 1 } return c })
</script>

<svelte:window onkeydown={key} />

<div class="shell">
  <header class="bar">
    <button class="ib menu" onclick={() => (app.showTray = !app.showTray)} aria-label="Drafts and filters"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 6h16M4 12h16M4 18h10" /></svg>{#if app.drafts.length}<span class="nb">{app.drafts.length}</span>{/if}</button>
    <span class="logo display"><img src="/logo-64.png" alt="" width="22" height="22" /><span class="lt">Townsquare</span></span>
    <span class="range display">{label}</span>
    <span class="nav" class:hide={app.view === 'stats'}>
    <button class="ib" onclick={() => shift(-1)} aria-label="Previous"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M15 18l-6-6 6-6" /></svg></button>
    <button class="ib" onclick={() => shift(1)} aria-label="Next"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 18l6-6-6-6" /></svg></button>
    <button class="ib" onclick={goToday}>Today</button>
    </span>
    <div class="seg">
      {#each [['week', 'Week'], ['time', 'Time'], ['month', 'Month'], ['list', 'List'], ['stats', 'Stats']] as [v, l]}<button class:on={app.view === v} onclick={() => setView(v)}>{l}</button>{/each}
    </div>
    <span class="sp"></span>
    <button class="search" onclick={() => (app.showSearch = true)} aria-label="Search posts">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="7" /><path d="M21 21l-4.3-4.3" /></svg>
      <span class="sl">Search posts</span><kbd>/</kbd>
    </button>
    {#if app.sending}<span class="sending">Sending: {app.sending}</span>{/if}
    <TZSlider />
    <button class="ib" class:on={app.showHistory} onclick={() => (app.showHistory = !app.showHistory)}><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M3 12a9 9 0 109-9 9 9 0 00-6.4 2.6L3 8" /><path d="M3 3v5h5M12 7v5l3 3" /></svg><span class="hlbl">History</span></button>
    <button class="ib" onclick={() => (app.showSettings = true)} aria-label="Settings"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.7 1.7 0 00.3 1.8l.1.1a2 2 0 11-2.8 2.8l-.1-.1a1.7 1.7 0 00-1.8-.3 1.7 1.7 0 00-1 1.5V21a2 2 0 11-4 0v-.1a1.7 1.7 0 00-1.1-1.5 1.7 1.7 0 00-1.8.3l-.1.1a2 2 0 11-2.8-2.8l.1-.1a1.7 1.7 0 00.3-1.8 1.7 1.7 0 00-1.5-1H3a2 2 0 110-4h.1a1.7 1.7 0 001.5-1.1 1.7 1.7 0 00-.3-1.8l-.1-.1a2 2 0 112.8-2.8l.1.1a1.7 1.7 0 001.8.3H9a1.7 1.7 0 001-1.5V3a2 2 0 114 0v.1a1.7 1.7 0 001 1.5 1.7 1.7 0 001.8-.3l.1-.1a2 2 0 112.8 2.8l-.1.1a1.7 1.7 0 00-.3 1.8V9a1.7 1.7 0 001.5 1H21a2 2 0 110 4h-.1a1.7 1.7 0 00-1.5 1z" /></svg></button>
    <button class="btn dark newb" onclick={newPost}>+ New post</button>
  </header>

  {#if app.demo}
    <div class="safe demo">Demo with sample data. Nothing here is real and nothing is ever sent.</div>
  {:else if app.settings.safe_mode === '1'}
    <div class="safe">Safe mode is on: only allowlisted chats receive posts. Everything else is held back. <button onclick={() => (app.showSettings = true)}>Manage</button></div>
  {/if}
  {#if app.viewTZ && app.viewTZ !== defaultTZ()}
    <div class="pvbar">Previewing all times in <b>{zoneName(tz())} ({tzShort(tz())})</b>. Your default is {zoneName(defaultTZ())}. New posts and moves use the zone you're viewing.
      <button onclick={() => { app.viewTZ = ''; loadSends() }}>Back to {zoneName(defaultTZ())}</button></div>
  {/if}
  {#if app.error}<div class="safe err">{app.error}</div>{/if}

  <div class="body">
    {#if app.showTray}<div class="trayscrim" role="presentation" onclick={() => (app.showTray = false)}></div>{/if}
    <aside class="tray" class:open={app.showTray}>
      <div class="label">Drafts <span>{app.drafts.length}</span></div>
      {#each app.drafts as d (d.id)}
        <div class="dr" draggable="true" role="button" tabindex="0" ondragstart={(e) => dragDraft(e, d)}
          onclick={() => (app.composer = { post: JSON.parse(JSON.stringify(d)), scope: 'all' })}
          onkeydown={(e) => e.key === 'Enter' && (app.composer = { post: JSON.parse(JSON.stringify(d)), scope: 'all' })}>
          <b>{d.title || clean(d.caption).split('\n')[0] || 'Untitled draft'}</b>
          <small>{d.targets.length} {d.targets.length === 1 ? 'target' : 'targets'}{d.media.length ? ` · ${d.media.length} media` : ''}</small>
        </div>
      {:else}
        <p class="muted hint">No drafts.</p>
      {/each}
      <div style="margin-top:14px"><CheckList label="Tags" items={app.tags} bind:hidden={app.hiddenTags} noneLabel="No tag" counts={tagCounts} /></div>
      {#if app.clients.length}
        <div style="margin-top:12px"><CheckList label="Clients" items={app.clients} bind:hidden={app.hiddenClients} noneLabel="No client" counts={clientCounts} /></div>
      {/if}
      <p class="muted hint" style="margin-top:auto">Drag a draft onto a day to schedule it. Hold ⌥ while dragging a post to copy it. Keys: / search, N new, W/G/M/L/S views, ←/→, ⌘Z.</p>
    </aside>
    <main>
      {#if !app.ready}<p class="muted" style="padding:24px">Loading…</p>
      {:else if app.view === 'week'}<Week />
      {:else if app.view === 'time'}<TimeGrid />
      {:else if app.view === 'month'}<Month />
      {:else if app.view === 'stats'}<Stats />
      {:else}<List />{/if}
      <History />
    </main>
  </div>
</div>

<button class="fab" onclick={newPost} aria-label="New post">+</button>
<Peek />
{#if app.composer}<Composer />{/if}
{#if app.showSettings}<Settings />{/if}
{#if app.showSearch}<Search />{/if}
<Toast />
{#if app.needSignIn}<SignIn />{/if}

<style>
  .nav.hide { visibility: hidden }
  .shell { display: flex; flex-direction: column; height: 100vh }
  .bar { display: flex; align-items: center; gap: 6px; height: 50px; padding: 0 12px 0 16px; border-bottom: 1px solid var(--line); background: var(--surface); flex: none }
  .logo { display: flex; align-items: center; gap: 8px; font-size: 22px; color: var(--t900); margin-right: 14px }
  .logo img { width: 22px; height: 22px; display: block }
  .range { font-size: 24px; min-width: 120px; margin-right: 4px }
  .seg { margin-left: 6px }
  .sp { flex: 1 }
  .sending { font-size: 12px; color: var(--t800); background: #E3F2EE; padding: 3px 8px; border-radius: 6px; max-width: 280px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis }
  .conn { display: inline-flex; align-items: center; gap: 5px; font: 500 11px var(--mono); color: var(--muted); margin: 0 4px }
  .dot { width: 7px; height: 7px; border-radius: 50%; background: var(--rose) } .dot.on { background: var(--grass) }
  .safe { background: var(--amber-bg); border-bottom: 1px solid var(--amber-line); color: var(--amber-ink); padding: 6px 16px; font-size: 12.5px; flex: none }
  .safe button { border: 0; background: transparent; color: var(--t800); font-weight: 600; text-decoration: underline }
  .safe.demo { background: #EEF6FD; border-color: #CFE3F5; color: #164E6D }
  .safe.err { background: #FDE4E7; color: #9B1C2C }
  .pvbar { background: #EEF6FD; border-bottom: 1px solid #CFE3F5; color: #164E6D; padding: 6px 16px; font-size: 12.5px; flex: none }
  .pvbar button { border: 0; background: transparent; color: #1B6A93; font-weight: 600; text-decoration: underline; margin-left: 4px }
  .body { flex: 1; display: grid; grid-template-columns: 196px minmax(0, 1fr); min-height: 0 }
  .tray { border-right: 1px solid var(--line); padding: 12px 10px; display: flex; flex-direction: column; gap: 5px; background: #FBFAF8; overflow: auto }
  .label { display: flex; justify-content: space-between; margin: 0 4px 2px }
  .dr { padding: 7px 8px; border-radius: 8px; background: var(--surface); border: 1px dashed #CFCAC0; cursor: grab }
  .dr:hover { border-color: var(--t600) }
  .dr b { display: block; font-weight: 500; font-size: 12.5px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis }
  .dr small { color: var(--muted); font-size: 11px }
  .hint { font-size: 11.5px; margin: 4px; line-height: 1.45 }
  .f { display: flex; align-items: center; gap: 7px; border: 0; background: transparent; padding: 3px 6px; border-radius: 6px; font-size: 12.5px; color: var(--ink2); text-align: left }
  .f:hover { background: var(--sunk) }
  .f.off { opacity: .4 }
  .sw { width: 9px; height: 9px; border-radius: 3px }
  main { position: relative; overflow: hidden; background: var(--surface) }
  .search { display: inline-flex; align-items: center; gap: 7px; border: 1px solid var(--line); background: #FBFAF8; border-radius: 8px; padding: 5px 8px 5px 9px; color: var(--muted); font-size: 12.5px; min-width: 190px; margin-right: 4px }
  .search:hover { border-color: #BFC7C3; color: var(--ink2) }
  .search svg { width: 14px; height: 14px }
  .search .sl { flex: 1; text-align: left }
  .search kbd { font: 500 10.5px var(--mono); border: 1px solid var(--line); border-radius: 4px; padding: 0 5px; background: #fff }
  .bar :global(.ib), .bar .btn, .logo, .range { white-space: nowrap }
  @media (max-width: 1280px) {
    .search { min-width: 0; padding: 6px } .search .sl, .search kbd { display: none }
    .hlbl { display: none }
    .logo { margin-right: 6px }
  }
  .menu { display: none; position: relative }
  .nb { position: absolute; top: 2px; right: 0; min-width: 15px; height: 15px; border-radius: 8px; background: var(--sky); color: #fff; font-size: 9.5px; font-weight: 700; display: grid; place-items: center; padding: 0 3px }
  .fab { display: none }
  .trayscrim { display: none }
  @media (max-width: 1000px) {
    .body { grid-template-columns: minmax(0, 1fr) }
    .menu { display: inline-flex }
    .tray { position: fixed; top: 0; bottom: 0; left: 0; width: 260px; z-index: 300; transform: translateX(-102%); transition: transform .25s var(--ease); box-shadow: 16px 0 40px -24px rgba(17,27,33,.4) }
    .tray.open { transform: none }
    .trayscrim { display: block; position: fixed; inset: 0; background: rgba(17,27,33,.2); z-index: 290 }
    .conn, .lt { display: none }
    .bar { height: auto; flex-wrap: wrap; padding: 6px 10px; row-gap: 4px }
    .logo, .newb, .sending { display: none }
    .search { min-width: 0; border: 0; background: transparent; padding: 6px } .search .sl, .search kbd { display: none }
    .hlbl { display: none }
    .range { font-size: 22px; margin-left: 2px; min-width: 150px; flex: 1 0 150px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis }
    .nav { order: 10; display: inline-flex }
    .seg { order: 11; flex: 1; margin: 0; display: flex }
    .seg button { flex: 1; padding: 6px }
    .sp { display: none }
    .fab { display: grid; place-items: center; position: fixed; right: 18px; bottom: 20px; width: 56px; height: 56px; border-radius: 50%; border: 0; background: var(--t800); color: #fff; font-size: 28px; line-height: 1; box-shadow: 0 10px 24px -8px rgba(6,48,43,.6); z-index: 200 }
    .bar { gap: 2px }
  }
  @media (max-width: 700px) {
    .bar { height: auto; flex-wrap: wrap; padding: 6px 8px; row-gap: 4px }
    .logo, .newb, .sending { display: none }
    .nav { order: 10; display: inline-flex }
    .search { min-width: 0; border: 0; background: transparent; padding: 6px } .search .sl, .search kbd { display: none }
    .range { font-size: 21px; margin-left: 2px; min-width: 150px; flex: 1 0 150px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis }
    .hlbl { display: none }
    .seg { order: 11; flex: 1; margin: 0; display: flex }
    .seg button { flex: 1; padding: 6px }
    .sp { display: none }
    .ib { min-width: 36px; height: 36px }
    .safe { font-size: 12px; padding: 6px 10px }
    .fab { display: grid; place-items: center; position: fixed; right: 16px; bottom: 18px; width: 54px; height: 54px; border-radius: 50%; border: 0; background: var(--t800); color: #fff; font-size: 28px; line-height: 1; box-shadow: 0 10px 24px -8px rgba(6,48,43,.6); z-index: 200 }
  }
</style>
