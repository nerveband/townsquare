<script>
  // Every draft at a glance: picture, text, chats, tag and client, with quick
  // actions. Opens from the Drafts list in the sidebar.
  import { app, api, act, tagOf, clientOf, target } from './state.svelte.js'
  import { waPlain } from './wafmt.js'
  import Thumb from './Thumb.svelte'
  import Avatars from './Avatars.svelte'
  import Platform from './Platform.svelte'

  let q = $state('')
  let sort = $state('updated') // updated, created, title
  let picked = $state([])
  const list = $derived.by(() => {
    const ql = q.trim().toLowerCase()
    const out = app.drafts.filter((d) => !ql || `${d.title} ${d.caption} ${d.targets.map((j) => target(j)?.name || '').join(' ')}`.toLowerCase().includes(ql))
    const by = { updated: (a, b) => b.updated_at - a.updated_at, created: (a, b) => b.created_at - a.created_at,
      title: (a, b) => (a.title || a.caption).localeCompare(b.title || b.caption) }[sort]
    return [...out].sort(by)
  })
  const ago = (t) => {
    const m = Math.round((Date.now() / 1000 - t) / 60)
    return m < 60 ? `${Math.max(1, m)} min ago` : m < 1440 ? `${Math.round(m / 60)} h ago` : `${Math.round(m / 1440)} days ago`
  }
  const MEDIA = { image: 'photo', video: 'video', voice: 'voice note', audio: 'audio', document: 'file' }
  function attached(d) {
    const c = {}
    for (const id of d.media) { const k = MEDIA[app.media[id]?.kind] || 'file'; c[k] = (c[k] || 0) + 1 }
    return Object.entries(c).map(([k, n]) => `${n} ${k}${n > 1 ? 's' : ''}`).join(', ')
  }
  function open(d) { app.showDrafts = false; app.composer = { post: JSON.parse(JSON.stringify(d)), scope: 'all' } }
  async function dup(d) { await act(api('POST', `/api/posts/${d.id}/duplicate`), 'Draft copied') }
  async function delMany(ids) {
    picked = []
    await act(api('POST', '/api/posts/bulk', { ids, action: 'delete' }), ids.length === 1 ? 'Draft deleted' : `${ids.length} drafts deleted`)
  }
  async function archiveMany(ids) { picked = []; await act(api('POST', '/api/posts/bulk', { ids, action: 'archive' }), `${ids.length} archived`) }
  const toggle = (id) => (picked = picked.includes(id) ? picked.filter((x) => x !== id) : [...picked, id])
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && !app.composer && (app.showDrafts = false)} />
<div class="scrim" role="presentation" onclick={() => (app.showDrafts = false)}></div>
<div class="modal" role="dialog" aria-label="Drafts">
  <header>
    <b class="display">Drafts</b><span class="muted">{app.drafts.length}</span>
    <input class="inp" type="search" bind:value={q} placeholder="Search drafts, text or chats" aria-label="Search drafts" />
    <span class="seg" role="radiogroup" aria-label="Sort">
      {#each [['updated', 'Recently edited'], ['created', 'Newest'], ['title', 'A to Z']] as [k, l]}<button class:on={sort === k} onclick={() => (sort = k)}>{l}</button>{/each}
    </span>
    <span class="sp"></span>
    <button class="ib" onclick={() => (app.showDrafts = false)} aria-label="Close">✕</button>
  </header>
  {#if picked.length}
    <div class="bulk"><b>{picked.length} selected</b>
      <button class="btn sm" onclick={() => archiveMany(picked)}>Archive</button>
      <button class="btn sm danger" onclick={() => delMany(picked)}>Delete</button>
      <button class="btn sm" onclick={() => (picked = [])}>Clear</button>
      <span class="muted">Undo works for all of these.</span></div>
  {/if}
  <div class="grid">
    {#each list as d (d.id)}
      <article class="card" class:sel={picked.includes(d.id)}>
        <label class="pick" title="Select"><input type="checkbox" checked={picked.includes(d.id)} onchange={() => toggle(d.id)} /></label>
        <button class="body" onclick={() => open(d)}>
          {#if d.media.length}<div class="th"><Thumb media={d.media} caption={d.caption} /></div>{/if}
          <b class="t">{d.title || waPlain(d.caption).split('\n')[0] || 'Untitled draft'}</b>
          {#if d.caption}<p class="cap">{waPlain(d.caption)}</p>{:else}<p class="cap muted">No text yet.</p>{/if}
        </button>
        <div class="meta">
          {#if tagOf(d.tag_id)}<span class="tag" style="--c:{tagOf(d.tag_id).color}">{tagOf(d.tag_id).name}</span>{/if}
          {#if clientOf(d.client_id)}<span class="tag" style="--c:{clientOf(d.client_id).color}">{clientOf(d.client_id).name}</span>{/if}
          {#if d.media.length}<span class="att">📎 {attached(d)}</span>{/if}
        </div>
        <div class="foot">
          {#if d.targets.some((j) => j.startsWith('tg'))}<Platform platform="telegram" />{/if}
          <Avatars jids={d.targets} max={4} size={16} />
          <span class="muted">{d.targets.length === 0 ? 'No chats yet' : d.targets.length === 1 ? target(d.targets[0])?.name || '1 chat' : `${d.targets.length} chats`}</span>
          <span class="sp"></span>
          <span class="muted when">{ago(d.updated_at || d.created_at)}</span>
        </div>
        <div class="acts">
          <button onclick={() => open(d)}>Open</button>
          <button onclick={() => dup(d)}>Copy</button>
          <button class="danger" onclick={() => delMany([d.id])}>Delete</button>
        </div>
      </article>
    {:else}
      <p class="muted empty">{q ? 'No drafts match.' : 'No drafts. Posts without a time, and anything saved as a draft, land here.'}</p>
    {/each}
  </div>
  <p class="muted tip">Tip: drag a draft from the sidebar onto a day to schedule it.</p>
</div>

<style>
  .scrim { position: fixed; inset: 0; background: rgba(17, 27, 33, .25); z-index: 400 }
  .modal { position: fixed; z-index: 401; inset: 4vh 4vw; display: flex; flex-direction: column; background: var(--surface); border-radius: 14px; box-shadow: var(--shadow); overflow: hidden }
  header { display: flex; align-items: center; gap: 10px; padding: 12px 16px; border-bottom: 1px solid var(--line2); flex-wrap: wrap }
  header b { font-size: 26px; color: var(--t900) }
  header .inp { max-width: 300px }
  .seg { display: inline-flex; background: var(--sunk); border-radius: 8px; padding: 2px }
  .seg button { border: 0; background: transparent; border-radius: 6px; padding: 4px 10px; font-size: 12px; color: var(--ink2) }
  .seg button.on { background: var(--surface); color: var(--ink); font-weight: 600; box-shadow: 0 1px 2px rgba(0,0,0,.08) }
  .sp { flex: 1 }
  .bulk { display: flex; align-items: center; gap: 8px; padding: 8px 16px; background: var(--note); font-size: 13px }
  .bulk .muted { font-size: 12px }
  .grid { flex: 1; overflow: auto; padding: 16px; display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 12px; align-content: start }
  .card { position: relative; border: 1.5px dashed #C9C4BA; border-radius: 12px; background: #FDFCFA; display: flex; flex-direction: column; overflow: hidden }
  .card.sel { border-style: solid; border-color: var(--sky); box-shadow: 0 0 0 3px rgba(22,115,230,.12) }
  .pick { position: absolute; top: 8px; right: 8px; z-index: 2; background: rgba(255,255,255,.9); border-radius: 6px; padding: 2px 4px; line-height: 0 }
  .body { border: 0; background: none; padding: 0; text-align: left; display: flex; flex-direction: column; cursor: pointer }
  .th { height: 120px; overflow: hidden }
  .t { padding: 10px 12px 0; font-size: 14px; color: var(--ink); display: block; padding-right: 34px }
  .cap { margin: 4px 12px 0; font-size: 12.5px; color: var(--ink2); display: -webkit-box; -webkit-line-clamp: 4; -webkit-box-orient: vertical; overflow: hidden; white-space: pre-line }
  .meta { display: flex; flex-wrap: wrap; gap: 5px; padding: 8px 12px 0 }
  .tag { font-size: 11px; padding: 1px 7px; border-radius: 10px; background: color-mix(in srgb, var(--c) 14%, white); color: color-mix(in srgb, var(--c) 70%, black) }
  .att { font-size: 11.5px; color: var(--ink2) }
  .foot { display: flex; align-items: center; gap: 6px; padding: 8px 12px; font-size: 12px }
  .when { font-size: 11px }
  .acts { display: flex; border-top: 1px dashed #DDD8CE; margin-top: auto }
  .acts button { flex: 1; border: 0; background: none; padding: 7px 0; font-size: 12.5px; color: var(--ink2) }
  .acts button:hover { background: var(--sunk) }
  .acts .danger { color: var(--rose) }
  .empty { grid-column: 1 / -1; text-align: center; padding: 40px 0 }
  .tip { padding: 0 16px 12px; font-size: 12px; margin: 0 }
  @media (max-width: 700px) { .modal { inset: 0; border-radius: 0 } header .inp { max-width: none } }
</style>
