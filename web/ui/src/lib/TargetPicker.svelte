<script>
  import { app, avatar, KIND_LABEL } from './state.svelte.js'
  import Platform from './Platform.svelte'
  let { selected = $bindable([]), compact = false } = $props()
  let q = $state('')
  let client = $state('')
  let platform = $state('')
  const hasTelegram = $derived(app.targets.some((t) => t.platform.startsWith('telegram')))
  const ORDER = ['self', 'status', 'announce', 'channel', 'group', 'community']
  const list = $derived.by(() => {
    const ql = q.trim().toLowerCase()
    return app.targets
      .filter((t) => !t.gone && t.kind !== 'community')
      .filter((t) => !client || String(t.client_id || '') === client)
      .filter((t) => !platform || t.platform.startsWith(platform))
      .filter((t) => !ql || t.name.toLowerCase().includes(ql) || (t.parent || '').toLowerCase().includes(ql))
      .sort((a, b) => (selected.includes(b.jid) - selected.includes(a.jid)) || (b.starred - a.starred) || (ORDER.indexOf(a.kind) - ORDER.indexOf(b.kind)) || a.name.localeCompare(b.name))
      .slice(0, 300)
  })
  function toggle(jid) {
    selected = selected.includes(jid) ? selected.filter((x) => x !== jid) : [...selected, jid]
  }
  function addSet(s) { selected = [...new Set([...selected, ...s.jids])] }
  const safe = $derived(app.settings.safe_mode === '1')
</script>

<div class="picker" class:compact>
  <div class="top">
    <input class="inp" type="search" placeholder="Search {app.targets.length} groups, channels…" bind:value={q} />
    {#if app.clients.length}
      <select class="inp" bind:value={client} aria-label="Client">
        <option value="">All clients</option>
        {#each app.clients as c}<option value={String(c.id)}>{c.name}</option>{/each}
      </select>
    {/if}
  </div>
  {#if hasTelegram}
    <div class="plat">
      {#each [['', 'All'], ['whatsapp', 'WhatsApp'], ['telegram', 'Telegram']] as [v, l]}<button class:on={platform === v} onclick={() => (platform = v)}>{l}</button>{/each}
    </div>
  {/if}
  {#if app.sets.length}
    <div class="sets">{#each app.sets as s}<button class="btn sm" onclick={() => addSet(s)}>+ {s.name} <span class="muted">{s.jids.length}</span></button>{/each}</div>
  {/if}
  <div class="rows">
    {#each list as t (t.jid)}
      {@const a = avatar(t.jid)}
      <label class:off={!t.can_send}>
        <input type="checkbox" checked={selected.includes(t.jid)} disabled={!t.can_send && !selected.includes(t.jid)} onchange={() => toggle(t.jid)} />
        <span class="av" style="background:{a.bg}">{a.ini}</span>
        {#if hasTelegram}<Platform platform={t.platform} />{/if}
        <span class="nm">{t.starred ? '★ ' : ''}{t.name}{#if t.parent && t.parent !== t.name}<i> · {t.parent}</i>{/if}</span>
        {#if safe && !t.allowed && t.can_send}<span class="flag" title="Safe mode is on: this target is not on the allowlist, so sends are held back">held</span>{/if}
        {#if !t.can_send}<span class="flag">admins only</span>{/if}
        <span class="k">{KIND_LABEL[t.kind] || t.kind}</span>
      </label>
    {:else}
      <p class="muted" style="padding:8px">No match. Groups refresh from WhatsApp in Settings.</p>
    {/each}
  </div>
</div>

<style>
  .picker { display: flex; flex-direction: column; gap: 6px; min-height: 0 }
  .top { display: flex; gap: 6px }
  .top select { width: 140px; flex: none }
  .sets { display: flex; flex-wrap: wrap; gap: 4px }
  .plat { display: flex; gap: 4px }
  .plat button { border: 1px solid var(--line); background: #fff; border-radius: 99px; padding: 2px 9px; font-size: 11.5px }
  .plat button.on { background: var(--t800); border-color: var(--t800); color: #fff }
  .rows { overflow: auto; max-height: 280px; border: 1px solid var(--line2); border-radius: 8px; padding: 3px }
  .compact .rows { max-height: 200px }
  label { display: flex; align-items: center; gap: 7px; padding: 4px 6px; border-radius: 6px; cursor: pointer; font-size: 12.5px }
  label:hover { background: var(--sunk) }
  label.off { opacity: .5; cursor: default }
  input[type=checkbox] { accent-color: var(--t800); margin: 0 }
  .av { border: 0; width: 18px; height: 18px }
  .nm { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0; flex: 1 }
  .nm i { color: var(--muted); font-style: normal }
  .k { font: 400 10px var(--mono); color: var(--muted); flex: none }
  .flag { font-size: 10px; color: #865A07; background: #FBF0D9; border-radius: 4px; padding: 0 5px; flex: none }
</style>
