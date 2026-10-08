<script>
  // Find any post, look at it, act on it, or act on many at once.
  import { onMount } from 'svelte'
  import { app, api, act, tz, target, tagOf, clientOf, avatar, toast } from './state.svelte.js'
  import { time12, prettyDay, dayKey, ruleLabel } from './time.js'
  import TargetPicker from './TargetPicker.svelte'
  import Avatars from './Avatars.svelte'
  import WaPreview from './WaPreview.svelte'
  import { waPlain } from './wafmt.js'

  let q = $state(app.searchQuery || '')
  let posts = $state([])
  let loading = $state(true)
  let statuses = $state(['scheduled', 'paused', 'draft'])
  let hideTags = $state([])
  let hideClients = $state([])
  let selected = $state([])
  let focus = $state(0)
  let view = $state(null) // post id shown in the quick view
  let next = $state([])
  let menu = $state('') // open bulk menu: tag, client, add, remove
  let pick = $state([])
  let input = $state()

  async function load() {
    posts = await api('GET', '/api/posts')
    loading = false
    const ids = [...new Set(posts.flatMap((p) => p.media))].filter((id) => !app.media[id])
    await Promise.all(ids.map(async (id) => { try { app.media[id] = await api('GET', `/api/media/${id}`) } catch {} }))
  }
  onMount(() => { load(); input?.focus() })

  const clean = (t) => waPlain(t)
  const STAT = [['scheduled', 'Scheduled'], ['paused', 'Paused'], ['draft', 'Drafts'], ['archived', 'Archived']]
  const results = $derived.by(() => {
    const words = q.trim().toLowerCase().split(/\s+/).filter(Boolean)
    return posts
      .filter((p) => statuses.includes(p.status))
      .filter((p) => !hideTags.includes(p.tag_id || 0) && !hideClients.includes(p.client_id || 0))
      .filter((p) => {
        if (!words.length) return true
        const hay = [p.title, clean(p.caption), tagOf(p.tag_id)?.name, clientOf(p.client_id)?.name, ...p.targets.map((j) => target(j).name)].join(' ').toLowerCase()
        return words.every((w) => hay.includes(w))
      })
      .sort((a, b) => (a.next_at && b.next_at ? a.next_at.localeCompare(b.next_at) : a.next_at ? -1 : b.next_at ? 1 : b.updated_at - a.updated_at))
  })
  const counts = $derived(Object.fromEntries(STAT.map(([k]) => [k, posts.filter((p) => p.status === k).length])))
  const allChecked = $derived(results.length > 0 && results.every((p) => selected.includes(p.id)))
  const current = $derived(posts.find((p) => p.id === view))

  $effect(() => {
    if (!view) return
    next = []
    api('GET', `/api/posts/${view}/next?n=5`).then((r) => (next = r)).catch(() => {})
  })
  $effect(() => { q; focus = 0 })

  function toggle(id) { selected = selected.includes(id) ? selected.filter((x) => x !== id) : [...selected, id] }
  function toggleAll() { selected = allChecked ? selected.filter((id) => !results.some((p) => p.id === id)) : [...new Set([...selected, ...results.map((p) => p.id)])] }
  function chip(list, v) { return list.includes(v) ? list.filter((x) => x !== v) : [...list, v] }

  async function bulk(action, extra = {}) {
    const ids = [...selected]
    menu = ''
    await act(api('POST', '/api/posts/bulk', { ids, action, ...extra }))
    if (action === 'delete') selected = []
    await load()
  }
  async function one(p, action) {
    if (action === 'edit') { close(); app.composer = { post: JSON.parse(JSON.stringify(p)), scope: 'all' }; return }
    if (action === 'duplicate') { await act(api('POST', `/api/posts/${p.id}/duplicate`)); await load(); return }
    if (action === 'send') { await act(api('POST', `/api/posts/${p.id}/send-now`), `Sending ${p.title || 'post'} now`); await load(); return }
    await act(api('POST', '/api/posts/bulk', { ids: [p.id], action }))
    if (action === 'delete' && view === p.id) view = null
    await load()
  }
  function close() { app.showSearch = false; app.searchQuery = q }

  function key(e) {
    if (e.key === 'Escape') { e.preventDefault(); if (menu) menu = ''; else if (view) view = null; else close(); return }
    if (e.target.closest('.menu')) return
    if (e.key === 'ArrowDown') { e.preventDefault(); focus = Math.min(focus + 1, results.length - 1); scrollTo() }
    else if (e.key === 'ArrowUp') { e.preventDefault(); focus = Math.max(focus - 1, 0); scrollTo() }
    else if (e.key === 'Enter' && results[focus]) { e.preventDefault(); view = results[focus].id }
    else if ((e.key === 'x' || e.key === ' ') && e.target !== input && results[focus]) { e.preventDefault(); toggle(results[focus].id) }
  }
  function scrollTo() { queueMicrotask(() => document.querySelector('.res .row.focus')?.scrollIntoView({ block: 'nearest' })) }

  function when(p) {
    if (p.status === 'draft') return 'Draft'
    if (p.status === 'archived') return 'Archived'
    if (!p.next_at) return p.last_at ? 'Done' : 'No upcoming sends'
    return `${prettyDay(dayKey(p.next_at, tz()))}, ${time12(p.next_at, tz(), true)}`
  }
  const rule = (p) => (p.schedules.find((s) => s.rrule) ? ruleLabel(p.schedules.find((s) => s.rrule).rrule) : p.schedules.length > 1 ? `${p.schedules.length} times` : '')
  const thumb = (p) => p.media.map((id) => app.media[id]).find((m) => m && (m.kind === 'image' || m.kind === 'video'))
</script>

<svelte:window onkeydown={key} />

<div class="scrim" role="presentation" onclick={close}></div>
<div class="modal" role="dialog" aria-label="Search posts">
  <div class="top">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="7" /><path d="M21 21l-4.3-4.3" /></svg>
    <input bind:this={input} bind:value={q} placeholder="Search posts by title, words, group, tag or client…" aria-label="Search" />
    <span class="muted">{results.length} of {posts.length}</span>
    <button class="ib" onclick={close} aria-label="Close"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 6L6 18M6 6l12 12" /></svg></button>
  </div>
  <div class="filters">
    {#each STAT as [k, l]}<button class="fc" class:on={statuses.includes(k)} onclick={() => (statuses = chip(statuses, k))}>{l} <i>{counts[k] || 0}</i></button>{/each}
    <span class="sep"></span>
    {#each [...app.tags, { id: 0, name: 'No tag', color: '#9AA5A1' }] as t (t.id)}
      <button class="fc" class:on={!hideTags.includes(t.id)} style="--c:{t.color}" onclick={() => (hideTags = chip(hideTags, t.id))}><span class="dot"></span>{t.name}</button>
    {/each}
    {#if app.clients.length}
      <span class="sep"></span>
      {#each [...app.clients, { id: 0, name: 'No client', color: '#9AA5A1' }] as c (c.id)}
        <button class="fc" class:on={!hideClients.includes(c.id)} style="--c:{c.color}" onclick={() => (hideClients = chip(hideClients, c.id))}><span class="dot sq"></span>{c.name}</button>
      {/each}
    {/if}
  </div>

  <div class="body" class:split={current}>
    <div class="res">
      <div class="head">
        <input type="checkbox" checked={allChecked} onchange={toggleAll} aria-label="Select all shown" />
        {#if selected.length}
          <b>{selected.length} selected</b>
          <div class="bulk">
            <button onclick={() => bulk('pause')}>Pause</button>
            <button onclick={() => bulk('resume')}>Resume</button>
            <button onclick={() => (menu = menu === 'tag' ? '' : 'tag')} class:on={menu === 'tag'}>Tag ▾</button>
            {#if app.clients.length}<button onclick={() => (menu = menu === 'client' ? '' : 'client')} class:on={menu === 'client'}>Client ▾</button>{/if}
            <button onclick={() => { pick = []; menu = menu === 'add' ? '' : 'add' }} class:on={menu === 'add'}>Add groups</button>
            <button onclick={() => { pick = []; menu = menu === 'remove' ? '' : 'remove' }} class:on={menu === 'remove'}>Remove groups</button>
            <button onclick={() => bulk('draft')}>To drafts</button>
            <button onclick={() => bulk('archive')}>Archive</button>
            <button class="danger" onclick={() => bulk('delete')}>Delete</button>
            <button class="clear" onclick={() => (selected = [])}>Clear</button>
          </div>
        {:else}
          <span class="muted">Select posts to change many at once. ↑↓ move · Enter open · X select</span>
        {/if}
      </div>
      {#if menu}
        <div class="menu">
          {#if menu === 'tag'}
            {#each [...app.tags, { id: null, name: 'No tag', color: '#9AA5A1' }] as t}<button class="fc on" style="--c:{t.color}" onclick={() => bulk('tag', { tag_id: t.id })}><span class="dot"></span>{t.name}</button>{/each}
          {:else if menu === 'client'}
            {#each [...app.clients, { id: null, name: 'No client', color: '#9AA5A1' }] as c}<button class="fc on" style="--c:{c.color}" onclick={() => bulk('client', { client_id: c.id })}><span class="dot sq"></span>{c.name}</button>{/each}
          {:else}
            <TargetPicker bind:selected={pick} compact />
            <div class="ma"><button class="btn sm pri" disabled={!pick.length} onclick={() => bulk(menu === 'add' ? 'add_targets' : 'remove_targets', { targets: pick })}>{menu === 'add' ? 'Add' : 'Remove'} {pick.length || ''} {pick.length === 1 ? 'chat' : 'chats'} {menu === 'add' ? 'to' : 'from'} {selected.length} {selected.length === 1 ? 'post' : 'posts'}</button></div>
          {/if}
        </div>
      {/if}
      <div class="list">
        {#if loading}<p class="muted pad">Loading…</p>{/if}
        {#each results as p, i (p.id)}
          {@const th = thumb(p)}
          <div class="row" class:focus={i === focus} class:sel={selected.includes(p.id)} class:open={view === p.id} role="button" tabindex="-1"
            onclick={() => { view = p.id; focus = i }} onkeydown={() => {}}>
            <input type="checkbox" checked={selected.includes(p.id)} onclick={(e) => e.stopPropagation()} onchange={() => toggle(p.id)} aria-label="Select {p.title}" />
            <span class="sw" style="background:{tagOf(p.tag_id)?.color || '#CFD6D3'}"></span>
            {#if th}<img src="/api/media/{th.id}/preview" alt="" onerror={(e) => e.currentTarget.classList.add('broken')} />{:else}<span class="ph">{p.media.length ? ({ voice: '♪', audio: '♪', document: 'Doc' }[app.media[p.media[0]]?.kind] || 'File') : 'Aa'}</span>{/if}
            <span class="tt"><b>{p.title || clean(p.caption).split('\n')[0] || 'Untitled'}</b><i>{clean(p.caption)}</i></span>
            <span class="tg"><Avatars jids={p.targets} max={3} size={16} /><em>{p.targets.length}</em></span>
            <span class="wh">{when(p)}{#if rule(p)}<small>↻ {rule(p)}</small>{/if}</span>
            <span class="st {p.status}">{p.status}</span>
            <span class="qa">
              <button title="Edit" onclick={(e) => { e.stopPropagation(); one(p, 'edit') }}>Edit</button>
              {#if p.status === 'scheduled'}<button title="Pause" onclick={(e) => { e.stopPropagation(); one(p, 'pause') }}>Pause</button>{:else if p.status === 'paused'}<button onclick={(e) => { e.stopPropagation(); one(p, 'resume') }}>Resume</button>{/if}
              <button title="Delete" class="danger" onclick={(e) => { e.stopPropagation(); one(p, 'delete') }}>Delete</button>
            </span>
          </div>
        {:else}
          {#if !loading}<p class="muted pad">No posts match. Try fewer words or turn on more filters.</p>{/if}
        {/each}
      </div>
    </div>

    {#if current}
      {@const p = current}
      <aside class="qv">
        <div class="qh">
          <b>{p.title || 'Untitled'}</b>
          <button class="ib" onclick={() => (view = null)} aria-label="Close quick view"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 6L6 18M6 6l12 12" /></svg></button>
        </div>
        <div class="tags">
          <span class="st {p.status}">{p.status}</span>
          {#if tagOf(p.tag_id)}<span class="pill" style="--c:{tagOf(p.tag_id).color}">{tagOf(p.tag_id).name}</span>{/if}
          {#if clientOf(p.client_id)}<span class="pill" style="--c:{clientOf(p.client_id).color}">{clientOf(p.client_id).name}</span>{/if}
        </div>
        <div class="chat"><WaPreview caption={p.caption} media={p.media} compact time={p.next_at ? time12(p.next_at, tz()) : ''} /></div>
        <h4>Sends to {p.targets.length}</h4>
        <div class="chips">{#each p.targets as j}{@const a = avatar(j)}<span class="chip"><span class="av" style="background:{a.bg}">{a.ini}</span><span>{a.name}</span></span>{/each}</div>
        <h4>When</h4>
        {#each p.schedules as s}<p class="sch">{prettyDay(s.start.slice(0, 10))} at {s.start.slice(11)} {s.tz.split('/').pop().replace('_', ' ')}{s.rrule ? ' · ↻ ' + ruleLabel(s.rrule) : ''}</p>{:else}<p class="sch muted">No time yet (draft)</p>{/each}
        {#if next.length}<div class="next">{#each next as o}<span class:past={new Date(o.at) < new Date()} class:sent={o.delivery === 'sent'}>{prettyDay(dayKey(o.at, tz()), false)} {time12(o.at, tz(), true)}{o.delivery === 'sent' ? ' ✓' : ''}</span>{/each}</div>{/if}
        <div class="acts">
          <button class="btn sm dark" onclick={() => one(p, 'edit')}>Edit</button>
          <button class="btn sm" onclick={() => one(p, 'duplicate')}>Duplicate</button>
          {#if p.status === 'scheduled'}<button class="btn sm" onclick={() => one(p, 'pause')}>Pause</button>{/if}
          {#if p.status === 'paused' || p.status === 'draft'}<button class="btn sm" onclick={() => one(p, 'resume')} disabled={p.status === 'draft'} title={p.status === 'draft' ? 'Open Edit to schedule a draft' : ''}>Resume</button>{/if}
          {#if p.status !== 'draft' && p.targets.length}<button class="btn sm" onclick={() => one(p, 'send')}>Send now</button>{/if}
          {#if p.status !== 'archived'}<button class="btn sm" onclick={() => one(p, 'archive')}>Archive</button>{/if}
          <button class="btn sm danger" onclick={() => one(p, 'delete')}>Delete</button>
        </div>
      </aside>
    {/if}
  </div>
</div>

<style>
  .scrim { position: fixed; inset: 0; background: rgba(17,27,33,.3); z-index: 490 }
  .modal { position: fixed; top: 6vh; left: 50%; transform: translateX(-50%); width: min(1180px, 96vw); height: 86vh; background: #fff; border-radius: 14px; box-shadow: var(--shadow); z-index: 500; display: flex; flex-direction: column; overflow: hidden; animation: pop .2s var(--ease) }
  @keyframes pop { from { opacity: 0; transform: translate(-50%, 8px) } }
  .top { display: flex; align-items: center; gap: 10px; padding: 12px 14px; border-bottom: 1px solid var(--line2) }
  .top svg { width: 18px; height: 18px; color: var(--muted); flex: none }
  .top input { flex: 1; border: 0; outline: none; font-size: 17px; background: transparent; min-width: 0 }
  .filters { display: flex; flex-wrap: wrap; gap: 5px; padding: 8px 14px; border-bottom: 1px solid var(--line2); background: #FBFAF8 }
  .fc { display: inline-flex; align-items: center; gap: 5px; border: 1px solid var(--line); background: #fff; border-radius: 99px; padding: 3px 9px; font-size: 12px; color: #A3ABAE }
  .fc.on { color: var(--ink); border-color: #CFD6D3 }
  .fc i { font: 500 10.5px var(--mono); font-style: normal; color: var(--muted) }
  .fc .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--c); opacity: .35 } .fc .dot.sq { border-radius: 2px }
  .fc.on .dot { opacity: 1 }
  .sep { width: 1px; background: var(--line); margin: 2px 4px }
  .body { flex: 1; display: grid; grid-template-columns: 1fr; min-height: 0 }
  .body.split { grid-template-columns: minmax(0, 1fr) 360px }
  .res { display: flex; flex-direction: column; min-height: 0; min-width: 0 }
  .head { display: flex; align-items: center; gap: 10px; padding: 8px 14px; border-bottom: 1px solid var(--line2); font-size: 12.5px; flex-wrap: wrap; min-height: 42px }
  .bulk { display: flex; gap: 3px; flex-wrap: wrap }
  .bulk button { border: 1px solid var(--line); background: #fff; border-radius: 7px; padding: 4px 9px; font-size: 12px }
  .bulk button:hover, .bulk button.on { border-color: var(--t600); color: var(--t800) }
  .bulk .danger { color: var(--rose) } .bulk .clear { border: 0; color: var(--muted) }
  .menu { padding: 10px 14px; border-bottom: 1px solid var(--line2); background: #FBFAF8; display: flex; flex-wrap: wrap; gap: 6px; flex-direction: column }
  .menu:has(.fc) { flex-direction: row }
  .ma { display: flex; justify-content: flex-end }
  .list { overflow: auto; flex: 1 }
  .pad { padding: 16px }
  input[type=checkbox] { accent-color: var(--t800); margin: 0; width: 15px; height: 15px }
  .row { display: grid; grid-template-columns: 16px 4px 36px minmax(0, 1fr) 64px 140px 80px 150px; gap: 10px; align-items: center; padding: 6px 14px; border-bottom: 1px solid var(--line2); cursor: pointer; font-size: 12.5px }
  .row:hover, .row.focus { background: #FAF9F6 }
  .row.sel { background: #F0F8F5 } .row.open { box-shadow: inset 3px 0 0 var(--t600) }
  .sw { width: 4px; height: 28px; border-radius: 2px }
  .row img, .ph { width: 36px; height: 36px; border-radius: 6px; object-fit: cover; background: var(--sunk); display: grid; place-items: center; font-size: 11px; color: var(--muted) }
  .tt { min-width: 0; display: flex; flex-direction: column }
  .tt b, .tt i { white-space: nowrap; overflow: hidden; text-overflow: ellipsis }
  .tt b { font-weight: 600 } .tt i { font-style: normal; color: var(--muted); font-size: 12px }
  .tg { display: flex; align-items: center; gap: 4px } .tg em { font-style: normal; color: var(--muted); font-size: 11.5px }
  .wh { display: flex; flex-direction: column; font-size: 12px; color: var(--ink2) } .wh small { color: var(--t800); font-size: 11px }
  .st { font-size: 11px; font-weight: 500; padding: 2px 7px; border-radius: 5px; text-transform: capitalize; justify-self: start; background: #E6F4EC; color: #11663A }
  .st.paused { background: #FBF0D9; color: #865A07 } .st.draft { background: #EEF0F1; color: #4A5960 } .st.archived { background: #F1EFEA; color: var(--muted) }
  .qa { display: flex; gap: 2px; opacity: 0; justify-content: flex-end }
  .row:hover .qa, .row.focus .qa { opacity: 1 }
  .qa button { border: 0; background: transparent; font-size: 11.5px; padding: 3px 6px; border-radius: 5px; color: var(--ink2) }
  .qa button:hover { background: var(--sunk) } .qa .danger { color: var(--rose) }
  .qv { border-left: 1px solid var(--line); overflow: auto; padding: 12px 14px; display: flex; flex-direction: column; gap: 8px; background: #FCFBF9 }
  .qh { display: flex; align-items: center; gap: 8px } .qh b { font: 600 22px/1 var(--display); text-transform: uppercase; flex: 1 }
  .tags { display: flex; gap: 5px; flex-wrap: wrap }
  .pill { font-size: 11px; padding: 2px 7px; border-radius: 5px; background: color-mix(in srgb, var(--c) 14%, #fff); color: color-mix(in srgb, var(--c) 70%, #000) }
  .chat { background: var(--chat); border-radius: 10px; padding: 10px; max-height: 420px; overflow: auto }
  .bub { background: var(--bubble); border-radius: 9px 2px 9px 9px; padding: 4px; max-width: 100%; box-shadow: 0 1px .5px rgba(0,0,0,.12) }
  .bub .media { display: grid; grid-template-columns: repeat(auto-fill, minmax(90px, 1fr)); gap: 3px; border-radius: 6px; overflow: hidden }
  .bub .media img { width: 100%; aspect-ratio: 1; object-fit: cover } .mf { aspect-ratio: 1; display: grid; place-items: center; background: #fff; font-size: 11px; color: var(--muted); text-transform: uppercase }
  .bub p { margin: 6px; white-space: pre-wrap; font-size: 13px }
  h4 { margin: 6px 0 0; font: 500 10.5px var(--mono); letter-spacing: .1em; text-transform: uppercase; color: var(--muted) }
  .chips { display: flex; flex-wrap: wrap; gap: 4px } .chip .av { width: 16px; height: 16px; font-size: 7px; border: 0 }
  .sch { margin: 0; font-size: 12.5px }
  .next { display: flex; flex-wrap: wrap; gap: 4px }
  .next span { font: 500 10.5px var(--mono); padding: 2px 6px; border-radius: 4px; background: var(--sunk) }
  .next span.past { opacity: .55 } .next span.sent { background: #E6F4EC }
  .acts { display: flex; flex-wrap: wrap; gap: 5px; margin-top: 6px; border-top: 1px solid var(--line2); padding-top: 10px }
  @media (max-width: 800px) {
    .modal { top: 0; height: 100%; width: 100%; border-radius: 0 }
    .body.split { grid-template-columns: 1fr }
    .body.split .res { display: none }
    .row { grid-template-columns: 16px 4px 36px minmax(0, 1fr) 74px; }
    .tg, .wh, .qa { display: none }
    .filters { flex-wrap: nowrap; overflow-x: auto; scrollbar-width: none }
    .fc { flex: none }
    .top input { font-size: 16px }
  }
</style>
