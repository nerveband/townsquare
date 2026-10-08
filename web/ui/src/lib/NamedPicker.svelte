<script>
  // Pick, create, rename, recolor or delete a tag or client, right where you use it.
  import { app, api, act } from './state.svelte.js'

  let { kind = 'tag', value = $bindable(null) } = $props()
  const path = kind === 'tag' ? '/api/tags' : '/api/clients'
  const noun = kind === 'tag' ? 'tag' : 'client'
  const list = $derived(kind === 'tag' ? app.tags : app.clients)
  const current = $derived(list.find((x) => x.id === value))
  const COLORS = ['#1673E6', '#0E2A47', '#2DB34E', '#0E8C8C', '#7C6BF0', '#C2557A', '#E0475B', '#E5A50A', '#8A6A3B', '#667781']

  let open = $state(false)
  let q = $state('')
  let editing = $state(null) // { id, name, color }
  let active = $state(0)
  let root = $state()
  let input = $state()

  const shown = $derived(list.filter((x) => x.name.toLowerCase().includes(q.trim().toLowerCase())))
  const exact = $derived(list.some((x) => x.name.toLowerCase() === q.trim().toLowerCase()))

  function toggle() {
    open = !open
    editing = null
    q = ''
    active = 0
    if (open) queueMicrotask(() => input?.focus())
  }
  function pick(id) {
    value = id
    open = false
  }
  async function create() {
    const name = q.trim()
    if (!name) return
    const color = COLORS[list.length % COLORS.length]
    const r = await act(api('POST', path, { name, color }), `Created ${noun} ${name}`)
    pick(r.id)
  }
  async function saveEdit() {
    if (!editing.name.trim()) return
    await act(api('PUT', `${path}/${editing.id}`, editing), `Updated ${noun} ${editing.name}`)
    editing = null
  }
  async function remove(x) {
    await act(api('DELETE', `${path}/${x.id}`), `Deleted ${noun} ${x.name}`)
    if (value === x.id) value = null
    editing = null
  }
  function onKey(e) {
    e.stopPropagation()
    if (e.key === 'Escape') { editing ? (editing = null) : (open = false) }
    else if (e.key === 'ArrowDown') { e.preventDefault(); active = Math.min(active + 1, shown.length) }
    else if (e.key === 'ArrowUp') { e.preventDefault(); active = Math.max(active - 1, 0) }
    else if (e.key === 'Enter' && !editing) {
      e.preventDefault()
      if (q.trim() && !exact && (active === 0 || !shown.length)) create()
      else if (shown[active - (q.trim() && !exact ? 1 : 0)]) pick(shown[active - (q.trim() && !exact ? 1 : 0)].id)
    }
  }
</script>

<svelte:window onclick={(e) => { if (open && root && !root.contains(e.target)) open = false }} />

<div class="np" bind:this={root}>
  <button class="trigger" class:set={current} onclick={toggle} aria-haspopup="listbox" aria-expanded={open} style="--c:{current?.color || '#9AA5A1'}">
    <span class="dot"></span>{current ? current.name : kind === 'tag' ? 'Add tag' : 'Add client'}
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4"><path d="M6 9l6 6 6-6" /></svg>
  </button>

  {#if open}
    <div class="pop" role="listbox" tabindex="-1" onkeydown={onKey}>
      {#if editing}
        <div class="edit">
          <input class="inp" bind:value={editing.name} aria-label="Name" onkeydown={(e) => { e.stopPropagation(); if (e.key === 'Enter') saveEdit(); if (e.key === 'Escape') editing = null }} />
          <div class="sw">
            {#each COLORS as c}<button class:on={editing.color === c} style="background:{c}" aria-label="Color {c}" onclick={() => (editing.color = c)}></button>{/each}
            <label class="custom" title="Custom color"><input type="color" bind:value={editing.color} />+</label>
          </div>
          <div class="row">
            <button class="btn sm danger" onclick={() => remove(editing)}>Delete</button>
            <span style="flex:1"></span>
            <button class="btn sm" onclick={() => (editing = null)}>Cancel</button>
            <button class="btn sm pri" onclick={saveEdit}>Save</button>
          </div>
          <p class="muted">Changes apply everywhere this {noun} is used. Undo is in History.</p>
        </div>
      {:else}
        <input class="inp" bind:this={input} bind:value={q} placeholder="Find or create a {noun}…" oninput={() => (active = 0)} />
        <div class="items">
          {#if q.trim() && !exact}
            <button class="it create" class:act={active === 0} onclick={create}><span class="plus">+</span>Create “{q.trim()}”</button>
          {/if}
          {#each shown as x, i (x.id)}
            {@const idx = i + (q.trim() && !exact ? 1 : 0)}
            <div class="it" class:act={active === idx} class:sel={x.id === value}>
              <button class="name" onclick={() => pick(x.id)}><span class="dot" style="background:{x.color}"></span>{x.name}{#if x.id === value}<span class="chk">✓</span>{/if}</button>
              <button class="ed" aria-label="Edit {x.name}" onclick={() => (editing = { ...x })}>
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 20h9M16.5 3.5a2.1 2.1 0 013 3L7 19l-4 1 1-4z" /></svg>
              </button>
            </div>
          {/each}
          {#if !shown.length && !q.trim()}<p class="muted" style="padding:6px 8px">No {noun}s yet. Type a name to create one.</p>{/if}
        </div>
        {#if value}<button class="clear" onclick={() => pick(null)}>Remove {noun}</button>{/if}
      {/if}
    </div>
  {/if}
</div>

<style>
  .np { position: relative }
  .trigger { display: inline-flex; align-items: center; gap: 6px; border: 1px dashed #CFCAC0; background: #fff; border-radius: 99px; padding: 4px 8px 4px 9px; font-size: 12px; color: var(--muted); white-space: nowrap; max-width: 160px }
  .trigger.set { border-style: solid; border-color: color-mix(in srgb, var(--c) 35%, #fff); background: color-mix(in srgb, var(--c) 10%, #fff); color: var(--ink) }
  .trigger svg { width: 12px; height: 12px; opacity: .6; flex: none }
  .trigger .dot { background: var(--c) }
  .dot { width: 9px; height: 9px; border-radius: 3px; flex: none }
  .pop { position: absolute; right: 0; top: calc(100% + 6px); width: 250px; background: #fff; border: 1px solid var(--line); border-radius: 12px; box-shadow: var(--shadow); padding: 8px; z-index: 30; display: flex; flex-direction: column; gap: 6px }
  .items { max-height: 260px; overflow: auto; display: flex; flex-direction: column }
  .it { display: flex; align-items: center; border-radius: 7px }
  .it.act, .it:hover { background: var(--sunk) }
  .name { flex: 1; display: flex; align-items: center; gap: 8px; border: 0; background: transparent; text-align: left; padding: 7px 8px; font-size: 13px; min-width: 0 }
  .chk { margin-left: auto; color: var(--t600); font-weight: 700 }
  .ed { border: 0; background: transparent; padding: 6px 8px; color: var(--muted); opacity: 0; border-radius: 6px }
  .it:hover .ed, .it.act .ed, .ed:focus-visible { opacity: 1 }
  .ed:hover { color: var(--ink); background: #E7E4DE }
  .ed svg { width: 13px; height: 13px; display: block }
  .create { border: 0; background: transparent; text-align: left; padding: 7px 8px; font-size: 13px; color: var(--t800); font-weight: 500; display: flex; gap: 8px; align-items: center }
  .plus { width: 9px; text-align: center }
  .clear { border: 0; border-top: 1px solid var(--line2); background: transparent; padding: 7px 4px 2px; text-align: left; font-size: 12px; color: var(--muted) }
  .clear:hover { color: var(--rose) }
  .edit { display: flex; flex-direction: column; gap: 8px }
  .sw { display: flex; flex-wrap: wrap; gap: 6px }
  .sw button, .custom { width: 22px; height: 22px; border-radius: 6px; border: 2px solid #fff; box-shadow: 0 0 0 1px var(--line) }
  .sw button.on { box-shadow: 0 0 0 2px var(--ink) }
  .custom { display: grid; place-items: center; font-size: 13px; color: var(--muted); background: var(--sunk); cursor: pointer; position: relative; overflow: hidden }
  .custom input { position: absolute; opacity: 0; inset: 0; cursor: pointer }
  .row { display: flex; gap: 6px; align-items: center }
  .muted { margin: 0; font-size: 11px }
  @media (hover: none) { .ed { opacity: 1 } }
  @media (max-width: 700px) { .pop { position: fixed; left: 8px; right: 8px; top: auto; bottom: 8px; width: auto } .trigger { max-width: none; padding: 7px 10px } }
</style>
