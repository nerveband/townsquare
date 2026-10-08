<script>
  // A small, quiet update badge in the top bar (in the spirit of Ghostty's
  // titlebar update notice). It never blocks: click it for the release notes.
  import { app, api, toast } from './state.svelte.js'
  import { md } from './md.js'

  const SEEN = 'townsquare.seenVersion'
  const SKIP = 'townsquare.skipVersion'
  let upd = $state(null)
  let open = $state(false)
  let busy = $state('')
  let after = $state(null) // { version, changes } shown once after an update
  let skipped = $state(localStorage.getItem(SKIP) || '')

  const isRelease = (v) => /^v\d+\.\d+\.\d+(-(alpha|beta|rc)\.?\d*)?$/.test(v || '')

  async function load() {
    try { upd = await api('GET', '/api/update') } catch { /* offline or restarting */ }
  }

  // After an update: show "Updated to vX" once, with what changed since the last version seen here.
  async function checkSeen(v) {
    if (!isRelease(v)) return
    const seen = localStorage.getItem(SEEN)
    if (!seen || !isRelease(seen)) { localStorage.setItem(SEEN, v); return } // first visit: nothing to announce
    if (seen === v) return
    try {
      const changes = await api('GET', `/api/changelog?from=${encodeURIComponent(seen)}&to=${encodeURIComponent(v)}`)
      if (changes.length) after = { version: v, changes }
    } catch {}
  }

  $effect(() => {
    if (app.demo || !app.version) return
    checkSeen(app.version)
    load()
    const t = setInterval(load, 15 * 60 * 1000)
    const vis = () => document.visibilityState === 'visible' && load()
    document.addEventListener('visibilitychange', vis)
    return () => { clearInterval(t); document.removeEventListener('visibilitychange', vis) }
  })

  // The server restarted into a new version (an automatic update) while this page was open.
  $effect(() => {
    if (!app.versionChanged) return
    const quiet = !app.composer && !app.showSettings && !open && !document.querySelector('input:focus, textarea:focus, [contenteditable]:focus')
    if (quiet) location.reload()
  })

  const kind = $derived.by(() => {
    if (busy === 'install') return 'installing'
    if (app.versionChanged) return 'reload'
    if (after) return 'after'
    if (!upd || upd.dev || !upd.latest) return ''
    if (upd.staged) return 'staged'
    if (upd.available && upd.latest !== skipped) return 'available'
    return ''
  })
  const label = $derived({
    installing: 'Updating…',
    reload: 'Reload',
    after: 'What’s new',
    staged: 'Restart to update',
    available: 'Update',
  }[kind])
  const notes = $derived(kind === 'after' ? after.changes : upd?.changes || [])

  async function install() {
    busy = 'install'
    try {
      let r
      try { r = await api('POST', '/api/update/install', {}) } catch (e) {
        if (!/due within|being sent/.test(e.message)) throw e
        busy = ''
        toast(`${e.message}. It installs on its own after that.`)
        return
      }
      if (r.up_to_date) { busy = ''; toast('Already up to date'); return }
      open = false
      for (let i = 0; i < 90; i++) {
        await new Promise((ok) => setTimeout(ok, 1500))
        try { const u = await api('GET', '/api/update'); if (u.current === r.version) { location.reload(); return } } catch {}
      }
      busy = ''
      toast('The update is taking a while. Reload the page in a minute.')
    } catch (e) { busy = ''; toast(e.message) }
  }
  async function turnOnAuto() {
    try { await api('PUT', '/api/settings', { auto_update: '1' }); await load(); toast('Updates now install on their own') } catch (e) { toast(e.message) }
  }
  function later() { if (kind === 'available') { localStorage.setItem(SKIP, upd.latest); skipped = upd.latest } open = false }
  function onPill() { if (kind === 'reload') location.reload(); else if (kind !== 'installing') open = !open }
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && open && (open = false)} />
{#if kind}
  <span class="upw">
    <button class="pill {kind}" onclick={onPill} aria-expanded={open} title={kind === 'reload' ? 'Townsquare was updated. Reload to use the new version.' : kind === 'after' ? `Updated to ${after?.version}` : kind === 'staged' ? `${upd.staged} is ready` : kind === 'available' ? `${upd.latest} is available` : ''}>
      <span class="dot"></span><span class="pl">{label}</span>
    </button>
    {#if open}
      <div class="scr" onclick={() => (open = false)} role="presentation"></div>
      <div class="pop" role="dialog" aria-label="Update">
        <div class="hd">
          {#if kind === 'after'}<b class="display">Updated to {after.version}</b><span class="muted">Here's what's new.</span>
          {:else if kind === 'staged'}<b class="display">Townsquare {upd.staged}</b><span class="muted">Downloaded and checked. It installs on its own when no post is due in the next 15 minutes.</span>
          {:else}<b class="display">Townsquare {upd.latest}</b><span class="muted">You have {upd.current}.{upd.auto ? ' It downloads and installs on its own soon.' : ''}</span>{/if}
        </div>
        <div class="notes">
          {#each notes as c (c.version)}
            <section><div class="ver mono">{c.version}{#if c.date}{' · ' + c.date}{/if}</div>{@html md(c.notes)}</section>
          {:else}
            <p class="muted">No release notes.</p>
          {/each}
        </div>
        <div class="ft">
          {#if kind === 'after'}
            <button class="btn pri sm" onclick={() => { localStorage.setItem(SEEN, after.version); after = null; open = false }}>Got it</button>
          {:else}
            <button class="btn pri sm" onclick={install} disabled={!!busy}>{kind === 'staged' ? 'Restart now' : 'Update now'}</button>
            {#if kind === 'available' && !upd.auto}<button class="btn sm" onclick={turnOnAuto}>Auto-update</button>{/if}
            <button class="btn sm" onclick={later}>Later</button>
          {/if}
          <span style="flex:1"></span>
          {#if upd?.notes && kind !== 'after'}<a class="muted" href={upd.notes} target="_blank" rel="noreferrer">On GitHub</a>{/if}
        </div>
      </div>
    {/if}
  </span>
{/if}

<style>
  .upw { position: relative; display: inline-flex }
  .pill { display: inline-flex; align-items: center; gap: 6px; height: 26px; padding: 0 10px; border-radius: 13px; border: 1px solid #CFE0F7; background: var(--sky-soft); color: var(--t800); font-size: 12px; font-weight: 600; white-space: nowrap; animation: in .4s var(--ease) }
  .pill:hover { filter: brightness(.97) }
  .pill .dot { width: 7px; height: 7px; border-radius: 50%; background: var(--sky) }
  .pill.after { background: #E9F7EE; border-color: #C9EBD4; color: #1E7A3A }
  .pill.after .dot { background: var(--grass) }
  .pill.installing .dot, .pill.reload .dot { animation: pulse 1s ease-in-out infinite }
  @keyframes pulse { 50% { opacity: .3 } }
  @keyframes in { from { opacity: 0; transform: translateY(-3px) } }
  .scr { position: fixed; inset: 0; z-index: 40 }
  .pop { position: absolute; top: 34px; right: 0; z-index: 41; width: min(380px, calc(100vw - 24px)); max-height: min(70vh, 560px); display: flex; flex-direction: column; background: var(--surface); border: 1px solid var(--line); border-radius: 12px; box-shadow: var(--shadow); animation: in .25s var(--ease) }
  .hd { padding: 14px 16px 10px; display: flex; flex-direction: column; gap: 4px; border-bottom: 1px solid var(--line2) }
  .hd b { font-size: 22px; color: var(--t900) }
  .hd .muted { font-size: 12px }
  .notes { overflow: auto; padding: 4px 16px 10px; font-size: 12.5px; color: var(--ink2) }
  .notes section + section { border-top: 1px dashed var(--line); margin-top: 8px }
  .ver { font-size: 11px; color: var(--muted); margin: 10px 0 2px }
  .notes :global(h4) { margin: 10px 0 4px; font: 600 11px/1 var(--mono); letter-spacing: .08em; text-transform: uppercase; color: var(--muted) }
  .notes :global(ul) { margin: 0; padding-left: 16px }
  .notes :global(li) { margin: 3px 0 }
  .notes :global(code) { font: 11.5px var(--mono); background: var(--sunk); padding: 0 3px; border-radius: 4px }
  .notes :global(p) { margin: 6px 0 }
  .ft { display: flex; gap: 6px; align-items: center; padding: 10px 16px; border-top: 1px solid var(--line2) }
  .ft a { font-size: 12px; white-space: nowrap }
  .ft .btn { white-space: nowrap }
  @media (max-width: 700px) {
    .pl { display: none }
    .pill { padding: 0 8px }
    .pop { position: fixed; top: 56px; right: 12px }
  }
</style>
