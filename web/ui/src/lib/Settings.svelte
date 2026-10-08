<script>
  import { app, api, act, loadTargets, avatar, KIND_LABEL, toast, target } from './state.svelte.js'
  import { tzShort } from './time.js'
  import TargetPicker from './TargetPicker.svelte'
  import Platform from './Platform.svelte'

  let tab = $state('general')
  let s = $state({ ...app.settings })
  let q = $state('')
  let kind = $state('')
  let onlyAllowed = $state(false)
  let refreshing = $state(false)
  let editSet = $state(null)

  const ZONES = ['America/Los_Angeles', 'America/Denver', 'America/Chicago', 'America/New_York', 'Europe/London', 'Asia/Dubai', 'Asia/Karachi', 'Asia/Kolkata', 'UTC']
  const COLORS = ['#1673E6', '#2DB34E', '#E5A50A', '#7C6BF0', '#0E8C8C', '#C2557A', '#E0475B', '#0E2A47', '#8A6A3B', '#667781']

  const dirty = $derived(Object.keys(s).some((k) => s[k] !== app.settings[k]))
  async function saveSettings() {
    const changed = Object.fromEntries(Object.entries(s).filter(([k, v]) => v !== app.settings[k] && k in app.settings))
    await act(api('PUT', '/api/settings', changed))
    s = { ...app.settings }
  }
  async function toggleSafe() {
    await act(api('PUT', '/api/settings', { safe_mode: app.settings.safe_mode === '1' ? '0' : '1' }))
    s = { ...app.settings }
  }
  async function refreshTargets() {
    refreshing = true
    try { app.targets = await api('POST', '/api/targets/refresh'); toast(`Loaded ${app.targets.length} groups and channels`) }
    catch (e) { toast(e.message) } finally { refreshing = false }
  }
  async function patchTarget(t, body) {
    await act(api('PATCH', `/api/targets/${encodeURIComponent(t.jid)}`, body))
    await loadTargets()
  }
  async function saveNamed(kindName, item) {
    const path = kindName === 'tag' ? '/api/tags' : '/api/clients'
    if (!item.name.trim()) return
    await act(item.id ? api('PUT', `${path}/${item.id}`, item) : api('POST', path, item))
  }
  async function delNamed(kindName, item) {
    await act(api('DELETE', `${kindName === 'tag' ? '/api/tags' : '/api/clients'}/${item.id}`))
  }
  let newTag = $state({ name: '', color: COLORS[5] })
  let newClient = $state({ name: '', color: COLORS[3] })

  async function saveSet() {
    const b = { name: editSet.name, client_id: editSet.client_id || null, jids: editSet.jids }
    await act(editSet.id ? api('PUT', `/api/sets/${editSet.id}`, b) : api('POST', '/api/sets', b))
    editSet = null
  }

  // Sortable columns. Click a heading to sort; click again to flip.
  let sortBy = $state('default')
  let sortDir = $state(1)
  const KORDER = ['self', 'status', 'announce', 'channel', 'group', 'community']
  const clientName = (t) => app.clients.find((c) => c.id === t.client_id)?.name || '\uffff'
  const SORTS = {
    default: (a, b) => KORDER.indexOf(a.kind) - KORDER.indexOf(b.kind) || a.name.localeCompare(b.name),
    starred: (a, b) => b.starred - a.starred || a.name.localeCompare(b.name),
    name: (a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: 'base', numeric: true }),
    kind: (a, b) => KORDER.indexOf(a.kind) - KORDER.indexOf(b.kind) || a.name.localeCompare(b.name),
    members: (a, b) => b.members - a.members || a.name.localeCompare(b.name),
    client: (a, b) => clientName(a).localeCompare(clientName(b)) || a.name.localeCompare(b.name),
    allowed: (a, b) => b.allowed - a.allowed || b.can_send - a.can_send || a.name.localeCompare(b.name),
  }
  function sort(col) {
    if (sortBy === col) sortDir = -sortDir
    else { sortBy = col; sortDir = 1 }
  }
  const arrow = (col) => (sortBy === col ? (sortDir === 1 ? '↓' : '↑') : '')
  const rows = $derived.by(() => {
    const ql = q.trim().toLowerCase()
    const cmp = SORTS[sortBy]
    return app.targets
      .filter((t) => !t.gone && (!kind || t.kind === kind) && (!onlyAllowed || t.allowed) && (!ql || t.name.toLowerCase().includes(ql)))
      .sort((a, b) => cmp(a, b) * sortDir)
      .slice(0, 400)
  })
  // Telegram account
  let tg = $state(null)
  let tgPw = $state('')
  let tgBusy = $state(false)
  let tgTimer
  async function loadTG() {
    try { tg = await api('GET', '/api/telegram') } catch (e) { tg = { status: 'error', error: e.message } }
    clearTimeout(tgTimer)
    if (tab === 'telegram' && tg && ['qr', 'password', 'starting'].includes(tg.status)) tgTimer = setTimeout(loadTG, 1500)
    if (tg?.status === 'ready') loadTargets()
  }
  $effect(() => { if (tab === 'telegram') loadTG(); return () => clearTimeout(tgTimer) })
  async function tgLogin() { tgBusy = true; try { tg = await api('POST', '/api/telegram/login'); loadTG() } catch (e) { toast(e.message) } finally { tgBusy = false } }
  async function tgSendPw() { tgBusy = true; try { tg = await api('POST', '/api/telegram/password', { password: tgPw }); tgPw = ''; loadTG() } catch (e) { toast(e.message) } finally { tgBusy = false } }
  async function tgLogout() { await act(api('POST', '/api/telegram/logout'), 'Logged out of Telegram'); loadTG() }
  async function tgRefresh() { tgBusy = true; try { tg = await api('POST', '/api/telegram/refresh'); await loadTargets(); toast('Telegram chats refreshed') } catch (e) { toast(e.message) } finally { tgBusy = false } }
  async function tgTest() { try { await api('POST', '/api/test', { platform: 'telegram', caption: '*Townsquare* test: this went to your Saved Messages only.' }); toast('Sent a test to your Telegram Saved Messages') } catch (e) { toast(e.message) } }

  // WhatsApp linking
  let wa = $state(null)
  let waPhone = $state('')
  let waBusy = $state(false)
  let waSure = $state(false)
  let waTimer
  async function loadWA() {
    try { wa = await api('GET', '/api/whatsapp') } catch { wa = null }
    clearTimeout(waTimer)
    const st = wa?.link?.state
    if (tab === 'general' && wa && !wa.linked && (st === 'waiting' || st === 'code')) waTimer = setTimeout(loadWA, 1500)
    if (tab === 'general' && wa?.linked && !wa.connected) waTimer = setTimeout(loadWA, 2000)
  }
  async function waLink() { waBusy = true; try { wa = await api('POST', '/api/whatsapp/link', { phone: waPhone }); loadWA() } catch (e) { toast(e.message) } finally { waBusy = false } }
  async function waLogout() { waSure = false; try { wa = await api('POST', '/api/whatsapp/logout'); toast('WhatsApp unlinked') } catch (e) { toast(e.message) } }

  // Updates, start at login, quit
  let upd = $state(null)
  let auto = $state(null)
  let updBusy = $state('')
  async function loadUpd() {
    try { upd = await api('GET', '/api/update') } catch { upd = null }
    try { auto = await api('GET', '/api/autostart') } catch { auto = null }
  }
  $effect(() => { if (tab === 'general') { loadWA(); loadUpd() } return () => clearTimeout(waTimer) })
  async function checkUpd() { updBusy = 'check'; try { upd = await api('POST', '/api/update/check') } catch (e) { toast(e.message) } finally { updBusy = '' } }
  async function installUpd(force = false) {
    updBusy = 'install'
    try {
      const r = await api('POST', '/api/update/install', { force })
      if (r.up_to_date) { toast('Already up to date'); updBusy = ''; return }
      toast(`Updating to ${r.version}. Townsquare restarts in a few seconds.`)
      const want = r.version
      for (let i = 0; i < 60; i++) {
        await new Promise((ok) => setTimeout(ok, 2000))
        try { const u = await api('GET', '/api/update'); if (u.current === want) { location.reload(); return } } catch {}
      }
      toast('The update is taking longer than expected. Reload the page in a minute.')
    } catch (e) {
      if (/due within|being sent/.test(e.message)) { toast(e.message); updBusy = 'busy' } else { toast(e.message); updBusy = '' }
      return
    }
    updBusy = ''
  }
  async function setAutoUpd(on) { await act(api('PUT', '/api/settings', { auto_update: on ? '1' : '0' })); loadUpd() }
  async function setLogin(on) { try { auto = await api('PUT', '/api/autostart', { enabled: on }); toast(on ? 'Townsquare will start when you log in' : 'Townsquare won’t start at login') } catch (e) { toast(e.message) } }
  async function quitApp() { try { await api('POST', '/api/quit'); document.body.innerHTML = '<p style="font:16px system-ui;padding:40px">Townsquare stopped. Open the app again to restart it.</p>' } catch (e) { toast(e.message) } }

  // Telegram app id
  let tgApp = $state({ api_id: '', api_hash: '' })
  async function tgSaveApp() { tgBusy = true; try { tg = await api('POST', '/api/telegram/app', tgApp); loadTG() } catch (e) { toast(e.message) } finally { tgBusy = false } }

  // API keys
  let keys = $state([])
  let newKey = $state({ name: '', scope: 'write' })
  let secret = $state(null)
  async function loadKeys() { keys = await api('GET', '/api/keys') }
  let sessions = $state([])
  async function loadSessions() { try { sessions = await api('GET', '/api/sessions') } catch { sessions = [] } }
  $effect(() => { if (tab === 'api') { loadKeys(); loadSessions() } })
  async function signOut(d) {
    if (d.current) { await api('POST', '/api/auth/logout'); location.reload(); return }
    await api('DELETE', `/api/sessions/${d.id}`); toast('Signed out that device'); loadSessions()
  }
  function deviceName(ua) {
    const os = /iPhone/.test(ua) ? 'iPhone' : /iPad/.test(ua) ? 'iPad' : /Android/.test(ua) ? 'Android' : /Mac OS X/.test(ua) ? 'Mac' : /Windows/.test(ua) ? 'Windows' : /Linux/.test(ua) ? 'Linux' : 'Device'
    const br = /Edg\//.test(ua) ? 'Edge' : /Chrome\//.test(ua) ? 'Chrome' : /Firefox\//.test(ua) ? 'Firefox' : /Safari\//.test(ua) ? 'Safari' : ''
    return br ? `${br} on ${os}` : os
  }
  async function createKey() {
    try {
      const r = await api('POST', '/api/keys', newKey)
      secret = { name: r.key.name, value: r.secret }
      newKey = { name: '', scope: 'write' }
      loadKeys()
    } catch (e) { toast(e.message) }
  }
  async function revokeKey(k) {
    try { await api('DELETE', `/api/keys/${k.id}`); toast(`Revoked ${k.name}`); loadKeys() } catch (e) { toast(e.message) }
  }
  const base = location.origin + '/api/v1'
  const allowedCount = $derived(app.targets.filter((t) => t.allowed).length)
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && !editSet && (app.showSettings = false)} />
<div class="scrim" onclick={() => (app.showSettings = false)} role="presentation"></div>
<div class="modal" role="dialog" aria-label="Settings">
  <nav>
    <b class="display">Settings</b>
    {#each [['general', 'General'], ['groups', 'Groups & safety'], ['clients', 'Clients'], ['tags', 'Tags'], ['sets', 'Group sets'], ['telegram', 'Telegram'], ['api', 'Access']] as [k, l]}
      <button class:on={tab === k} onclick={() => (tab = k)}>{l}</button>
    {/each}
    <span style="flex:1"></span>
    <button onclick={() => (app.showSettings = false)}>Close</button>
  </nav>
  <div class="pane">
    {#if tab === 'general'}
      <h3>WhatsApp</h3>
      {#if wa && !wa.linked && !app.demo}
        {@const ls = wa.link?.state}
        {#if ls === 'code' || ls === 'waiting'}
          <p>On your phone, open <b>WhatsApp → Settings → Linked devices → Link a device</b> and scan this code. It refreshes on its own.</p>
          {#if ls === 'code'}<img class="tgqr" src="/api/whatsapp/qr.png?v={wa.link.version}" alt="WhatsApp link QR code" />{:else}<p class="muted">Getting a code…</p>{/if}
          {#if wa.link.pair_code}<p>Or choose <b>Link with phone number instead</b> and enter <b class="mono">{wa.link.pair_code}</b></p>{/if}
        {:else if ls === 'linked'}
          <p class="st"><span class="dot on"></span>Linked. Connecting and loading your groups…</p>
        {:else}
          <p>Link your WhatsApp account, the same way you'd link WhatsApp Web. Townsquare then posts as you to the groups, channels and Status you choose.</p>
          <div class="row">
            <button class="btn pri" onclick={waLink} disabled={waBusy}>{waBusy ? 'Starting…' : 'Link WhatsApp'}</button>
            <input class="inp" bind:value={waPhone} placeholder="Phone number (optional)" style="max-width:220px" aria-label="Phone number with country code, for a code instead of a QR" />
            <span class="muted">Add your number with country code to also get a code you can type in.</span>
          </div>
          {#if wa.link?.state === 'error'}<p class="err">{wa.link.message}</p>{/if}
        {/if}
      {:else}
        <p class="st"><span class="dot" class:on={app.connected}></span>{app.connected ? 'Connected' : 'Not connected'} {#if app.phone}as +{app.phone}{/if} · linked device “Townsquare” · sent today: {app.sentToday}
          {#if wa?.linked && !app.demo}
            <span style="flex:1"></span>
            {#if waSure}<button class="btn danger sm" onclick={waLogout}>Unlink now</button><button class="btn sm" onclick={() => (waSure = false)}>Keep</button>
            {:else}<button class="btn sm" onclick={() => (waSure = true)}>Unlink</button>{/if}
          {/if}
        </p>
      {/if}

      <h3>Townsquare</h3>
      <p class="st">Version {upd?.current || app.version || 'dev'}
        {#if upd && !upd.dev && !app.demo && (upd.staged || upd.available || (upd.checked_at && !upd.error))}
          · {#if upd.staged}<b>{upd.staged}</b> is downloaded and starts soon{:else if upd.available}<b>{upd.latest}</b> is available{#if upd.notes} (<a href={upd.notes} target="_blank" rel="noreferrer">what's new</a>){/if}{:else}up to date{/if}
        {:else if upd?.dev && upd.latest && upd.available}
          · release {upd.latest} is out (this copy is built from source)
        {/if}
        · <a href="/api/v1/docs" target="_blank" rel="noreferrer">API docs</a>
      </p>
      {#if upd?.error}<p class="err">Couldn't check for updates: {upd.error}</p>{/if}
      {#if upd && !app.demo}
        <div class="row">
          <button class="btn" onclick={checkUpd} disabled={!!updBusy}>{updBusy === 'check' ? 'Checking…' : 'Check for updates'}</button>
          {#if !upd.dev && (upd.available || upd.staged)}
            {#if updBusy === 'busy'}<button class="btn pri" onclick={() => installUpd(true)}>Update anyway</button>
            {:else}<button class="btn pri" onclick={() => installUpd()} disabled={!!updBusy}>{updBusy === 'install' ? 'Updating…' : 'Update now'}</button>{/if}
          {/if}
          {#if !upd.dev}<label class="ck"><input type="checkbox" checked={upd.auto} onchange={(e) => setAutoUpd(e.target.checked)} /> Install updates automatically</label>{/if}
          {#if auto?.supported}<label class="ck"><input type="checkbox" checked={auto.enabled} onchange={(e) => setLogin(e.target.checked)} /> Start when I log in</label>{/if}
          {#if auto?.app_mode}<span style="flex:1"></span><button class="btn" onclick={quitApp}>Quit Townsquare</button>{/if}
        </div>
        <p class="muted">Automatic updates are checked every 6 hours, signed and verified, and installed only when no post is due within 15 minutes.{#if upd.dev} This copy was built from source, so update it with git pull and a rebuild.{/if}</p>
      {/if}

      <h3>Safe mode</h3>
      <div class="safe" class:on={app.settings.safe_mode === '1'}>
        <div><b>{app.settings.safe_mode === '1' ? 'On' : 'Off'}</b>: {app.settings.safe_mode === '1' ? `only the ${allowedCount} allowlisted chats receive anything. Everything else is held back and shown as “Held”.` : 'scheduled posts go to every chosen group.'}</div>
        <button class="btn {app.settings.safe_mode === '1' ? '' : 'pri'}" onclick={toggleSafe}>{app.settings.safe_mode === '1' ? 'Turn off' : 'Turn on'}</button>
      </div>

      <h3>Time zone</h3>
      <div class="row">
        <select class="inp" bind:value={s.timezone} style="max-width:260px">
          {#each [...new Set([s.timezone, ...ZONES])] as z}<option value={z}>{z.replace('_', ' ')} ({tzShort(z)})</option>{/each}
        </select>
        <span class="muted">The calendar shows this zone. Each post keeps the zone it was scheduled in.</span>
      </div>

      <h3>Pacing</h3>
      <div class="grid">
        <label>Gap between chats<span><input class="inp" type="number" min="0" bind:value={s.gap_min} /> to <input class="inp" type="number" min="0" bind:value={s.gap_max} /> sec</span></label>
        <label>Daily limit<span><input class="inp" type="number" min="1" bind:value={s.daily_cap} /> sends</span></label>
        <label>Quiet hours<span><input class="inp" type="time" bind:value={s.quiet_start} /> to <input class="inp" type="time" bind:value={s.quiet_end} /></span></label>
        <label>Late-send grace<span><input class="inp" type="number" min="1" bind:value={s.grace_min} /> min</span></label>
      </div>
      <p class="muted">If Townsquare or WhatsApp is offline at send time, it still sends within the grace window, then marks the send as missed instead of posting late.</p>
      <div class="row"><button class="btn pri" disabled={!dirty} onclick={saveSettings}>Save settings</button></div>
    {:else if tab === 'groups'}
      <div class="row">
        <input class="inp" type="search" placeholder="Search {app.targets.length}…" bind:value={q} style="max-width:240px" />
        <select class="inp" bind:value={kind} style="max-width:150px"><option value="">All kinds</option>{#each Object.entries(KIND_LABEL) as [k, l]}<option value={k}>{l}</option>{/each}</select>
        <label class="ck"><input type="checkbox" bind:checked={onlyAllowed} /> Allowlisted only</label>
        <span style="flex:1"></span>
        <button class="btn" onclick={refreshTargets} disabled={refreshing || !app.connected}>{refreshing ? 'Loading…' : 'Refresh from WhatsApp'}</button>
      </div>
      <p class="muted">Allowlist = chats that may receive posts while safe mode is on. Assign a client to filter groups and posts by client.</p>
      <div class="tbl">
        <div class="tr th" role="row">
          <button class="sorth" onclick={() => sort('starred')} aria-label="Sort by starred" title="Sort by starred">★{arrow('starred')}</button>
          <span></span>
          <button class="sorth" onclick={() => sort('name')}>Name {arrow('name')}</button>
          <span class="sortpair"><button class="sorth" onclick={() => sort('kind')}>Kind {arrow('kind')}</button><button class="sorth" onclick={() => sort('members')}>Members {arrow('members')}</button></span>
          <button class="sorth" onclick={() => sort('client')}>Client {arrow('client')}</button>
          <button class="sorth" onclick={() => sort('allowed')}>Allowed {arrow('allowed')}</button>
        </div>
        {#each rows as t (t.jid)}
          {@const a = avatar(t.jid)}
          <div class="tr" class:off={!t.can_send}>
            <button class="star" class:on={t.starred} title="Star (shows first in pickers)" onclick={() => patchTarget(t, { starred: !t.starred })}>★</button>
            <span class="av" style="background:{a.bg}">{a.ini}</span>
            <span class="nm">{#if t.platform === 'telegram'}<Platform platform="telegram" /> {/if}{t.name}{#if t.parent && t.parent !== t.name}<i> · {t.parent}</i>{/if}</span>
            <span class="k">{KIND_LABEL[t.kind] || t.kind}{t.members ? ' · ' + t.members : ''}{t.can_send ? '' : ' · admins only'}</span>
            <select class="inp sm" value={t.client_id ?? ''} onchange={(e) => patchTarget(t, e.target.value ? { client_id: +e.target.value } : { no_client: true })} aria-label="Client">
              <option value="">No client</option>{#each app.clients as c}<option value={c.id}>{c.name}</option>{/each}
            </select>
            <label class="ck"><input type="checkbox" checked={t.allowed} onchange={() => patchTarget(t, { allowed: !t.allowed })} /> Allow</label>
          </div>
        {/each}
      </div>
    {:else if tab === 'clients' || tab === 'tags'}
      {@const list = tab === 'tags' ? app.tags : app.clients}
      {@const k = tab === 'tags' ? 'tag' : 'client'}
      <p class="muted">{tab === 'tags' ? 'Tags color posts on the calendar.' : 'Clients group posts and chats, so you can filter the calendar to one client.'} Edits save when you leave a field. Undo works here too.</p>
      {#each list as item (item.id)}
        <div class="row named">
          <input type="color" value={item.color} onchange={(e) => saveNamed(k, { ...item, color: e.target.value })} aria-label="Color" />
          <input class="inp" value={item.name} onchange={(e) => saveNamed(k, { ...item, name: e.target.value })} aria-label="Name" />
          {#if k === 'client'}
            <span class="muted">{app.targets.filter((t) => t.client_id === item.id).length} chats</span>
            {@const mode = !item.quiet_start ? 'global' : item.quiet_start === item.quiet_end ? 'none' : 'custom'}
            <span class="qseg" role="radiogroup" aria-label="Quiet hours for {item.name}">
              <span class="muted">Quiet hours</span>
              <button class:on={mode === 'global'} title="Use global ({app.settings.quiet_start} to {app.settings.quiet_end})" onclick={() => saveNamed(k, { ...item, quiet_start: '', quiet_end: '', timezone: '' })}>Global</button>
              <button class:on={mode === 'custom'} onclick={() => mode !== 'custom' && saveNamed(k, { ...item, quiet_start: '22:00', quiet_end: '07:00', timezone: item.timezone || app.settings.timezone })}>Custom</button>
              <button class:on={mode === 'none'} onclick={() => saveNamed(k, { ...item, quiet_start: '00:00', quiet_end: '00:00', timezone: '' })}>None</button>
            </span>
            {#if item.quiet_start && item.quiet_start !== item.quiet_end}
              <input class="inp sm qt" type="time" value={item.quiet_start} onchange={(e) => saveNamed(k, { ...item, quiet_start: e.target.value })} aria-label="Quiet from" />
              <span class="muted">to</span>
              <input class="inp sm qt" type="time" value={item.quiet_end} onchange={(e) => saveNamed(k, { ...item, quiet_end: e.target.value })} aria-label="Quiet until" />
              <select class="inp sm qz" value={item.timezone || app.settings.timezone} onchange={(e) => saveNamed(k, { ...item, timezone: e.target.value })} aria-label="Time zone">
                {#each [...new Set([item.timezone || app.settings.timezone, ...ZONES])] as z}<option value={z}>{z.split('/').pop().replace('_', ' ')} ({tzShort(z)})</option>{/each}
              </select>
            {/if}
          {/if}
          <button class="btn danger sm" onclick={() => delNamed(k, item)}>Delete</button>
        </div>
      {/each}
      {@const nw = tab === 'tags' ? newTag : newClient}
      <div class="row named">
        <input type="color" bind:value={nw.color} aria-label="Color" />
        <input class="inp" bind:value={nw.name} placeholder="New {k} name" onkeydown={(e) => e.key === 'Enter' && saveNamed(k, nw).then(() => (nw.name = ''))} />
        <button class="btn pri sm" disabled={!nw.name.trim()} onclick={() => saveNamed(k, nw).then(() => (nw.name = ''))}>Add {k}</button>
      </div>
    {:else if tab === 'telegram'}
      <h3>Telegram</h3>
      {#if !tg}
        <p class="muted">Loading…</p>
      {:else if !tg.configured}
        <p>Telegram needs a free app ID. Sign in at <a href="https://my.telegram.org" target="_blank" rel="noreferrer">my.telegram.org</a>, open <b>API development tools</b>, create an app (any name), and copy the two values here.</p>
        <div class="row">
          <input class="inp" bind:value={tgApp.api_id} placeholder="api_id (a number)" inputmode="numeric" style="max-width:180px" aria-label="api_id" />
          <input class="inp" bind:value={tgApp.api_hash} placeholder="api_hash" style="max-width:300px" aria-label="api_hash" />
          <button class="btn pri" onclick={tgSaveApp} disabled={tgBusy || !tgApp.api_id || !tgApp.api_hash}>Save</button>
        </div>
      {:else if tg.status === 'ready'}
        <p class="st"><span class="dot on"></span>Logged in as <b>{tg.user}</b>{tg.username ? ` (@${tg.username})` : ''} · {tg.chats} chats · {tg.allowlisted} allowlisted</p>
        <p class="muted">Posts go out as you in groups, and as the channel in channels. New Telegram chats start off the allowlist; allow them in Groups &amp; safety. Saved Messages is your own chat, safe for tests.</p>
        <div class="row"><button class="btn" onclick={tgRefresh} disabled={tgBusy}>Refresh chats</button><button class="btn" onclick={tgTest}>Send test to Saved Messages</button><span style="flex:1"></span><button class="btn danger" onclick={tgLogout}>Log out</button></div>
        <h3>Telegram's own queue</h3>
        <p class="muted">Hand Telegram posts to Telegram's servers ahead of time, so they go out even if this computer is off or asleep. Edits, moves, pauses and undo update what's queued. Stories always send live.</p>
        <div class="qseg">
          {#each [[0, 'Off'], [24, '24 hours ahead'], [48, '48 hours ahead'], [168, '7 days ahead']] as [h, l]}
            <button class:on={(tg.queue_hours || 0) === h} onclick={async () => { await act(api('PUT', '/api/settings', { tg_queue_hours: String(h) }), h ? `Telegram queue: ${l}` : 'Telegram queue off'); loadTG() }}>{l}</button>
          {/each}
        </div>
      {:else if tg.status === 'qr'}
        <p>On your phone, open <b>Telegram → Settings → Devices → Link Desktop Device</b> and scan this code. It refreshes on its own.</p>
        <img class="tgqr" src="/api/telegram/qr.png?v={tg.version}" alt="Telegram login QR code" />
      {:else if tg.status === 'password'}
        <p>Your account has two-step verification. Enter your Telegram password to finish.</p>
        <div class="row"><input class="inp" type="password" bind:value={tgPw} placeholder="Telegram password" style="max-width:260px" onkeydown={(e) => e.key === 'Enter' && tgPw && tgSendPw()} /><button class="btn pri" disabled={!tgPw || tgBusy} onclick={tgSendPw}>Log in</button></div>
        {#if tg.error}<p class="err">{tg.error}</p>{/if}
      {:else}
        <p>Log in with your own Telegram account, the same way you'd link Telegram Desktop. Townsquare can then post to the groups and channels you manage.</p>
        <div class="row"><button class="btn pri" onclick={tgLogin} disabled={tgBusy || tg.status === 'starting'}>{tg.status === 'starting' ? 'Connecting…' : 'Log in with QR code'}</button></div>
        {#if tg.error}<p class="err">{tg.error}</p>{/if}
      {/if}
      {#if tg?.bot}
        <h3>Telegram bot</h3>
        <p class="st"><span class="dot on"></span><b>{tg.bot.name}</b> (@{tg.bot.username}) · {tg.bot.chats} chats</p>
        <p class="muted">Optional. Add @{tg.bot.username} as an admin to a group or channel and it shows up here with a “TG bot” badge. Send it /start to get a private test chat. Bot chats start off the allowlist.</p>
      {/if}
    {:else if tab === 'api'}
      <h3>Signed-in devices</h3>
      <p class="muted">Browsers that can use this web app. New devices sign in with a link sent to your WhatsApp.</p>
      {#each sessions as d (d.id)}
        <div class="row named">
          <b style="min-width:160px">{deviceName(d.device)}{d.current ? ' (this device)' : ''}</b>
          <span class="muted" style="flex:1">last used {new Date(d.last_seen * 1000).toLocaleString()}</span>
          <button class="btn danger sm" onclick={() => signOut(d)}>{d.current ? 'Sign out' : 'Sign out device'}</button>
        </div>
      {:else}
        <p class="muted">No devices (demo mode doesn't need sign-in).</p>
      {/each}
      <h3>REST API</h3>
      <p class="muted">Everything in Townsquare is available to scripts and AI agents at <code>{base}</code>. Give the agent the guide first.</p>
      <div class="row links">
        <a class="btn sm" href="/api/v1/docs" target="_blank" rel="noreferrer">Interactive docs</a>
        <a class="btn sm" href="/api/v1/openapi.json" target="_blank" rel="noreferrer">OpenAPI 3.1 spec</a>
        <a class="btn sm" href="/api/v1/guide.md" target="_blank" rel="noreferrer">Agent guide (Markdown)</a>
      </div>
      <h3>Keys</h3>
      <p class="muted"><b>read</b>: look only. <b>write</b>: create, edit, schedule, undo. <b>admin</b>: also safe mode, allowlist and keys. Changes show in History as “API key name”.</p>
      {#if secret}
        <div class="secret">
          <b>Copy the key for “{secret.name}” now. It won't be shown again.</b>
          <div class="row"><code class="sv">{secret.value}</code><button class="btn sm pri" onclick={() => { navigator.clipboard?.writeText(secret.value); toast('Copied') }}>Copy</button><button class="btn sm" onclick={() => (secret = null)}>Done</button></div>
          <code class="sv small">curl -H "Authorization: Bearer {secret.value}" {base}/status</code>
        </div>
      {/if}
      <div class="row named">
        <input class="inp" bind:value={newKey.name} placeholder="Key name, e.g. claude or n8n" onkeydown={(e) => e.key === 'Enter' && newKey.name.trim() && createKey()} />
        <select class="inp" bind:value={newKey.scope} style="max-width:120px"><option value="read">read</option><option value="write">write</option><option value="admin">admin</option></select>
        <button class="btn pri sm" disabled={!newKey.name.trim()} onclick={createKey}>Create key</button>
      </div>
      {#each keys as k (k.id)}
        <div class="row named key" class:revoked={k.revoked_at}>
          <b style="min-width:140px">{k.name}</b><span class="scope">{k.scope}</span><code>{k.prefix}…</code>
          <span class="muted" style="flex:1">{k.revoked_at ? 'revoked' : k.last_used_at ? 'used ' + new Date(k.last_used_at * 1000).toLocaleString() : 'never used'}</span>
          {#if !k.revoked_at}<button class="btn danger sm" onclick={() => revokeKey(k)}>Revoke</button>{/if}
        </div>
      {/each}
    {:else if tab === 'sets'}
      {#if editSet}
        <div class="row"><input class="inp" bind:value={editSet.name} placeholder="Set name, e.g. All chapter groups" />
          <select class="inp" bind:value={editSet.client_id} style="max-width:180px"><option value={null}>No client</option>{#each app.clients as c}<option value={c.id}>{c.name}</option>{/each}</select></div>
        <TargetPicker bind:selected={editSet.jids} />
        <div class="row"><button class="btn pri" disabled={!editSet.name.trim() || !editSet.jids.length} onclick={saveSet}>Save set ({editSet.jids.length})</button><button class="btn" onclick={() => (editSet = null)}>Cancel</button></div>
      {:else}
        <p class="muted">A group set adds many chats to a post in one click.</p>
        {#each app.sets as st (st.id)}
          <div class="row named">
            <b style="min-width:180px">{st.name}</b>
            <span class="muted" style="flex:1">{st.jids.slice(0, 4).map((j) => target(j).name).join(', ')}{st.jids.length > 4 ? ` +${st.jids.length - 4}` : ''}</span>
            <button class="btn sm" onclick={() => (editSet = JSON.parse(JSON.stringify(st)))}>Edit</button>
            <button class="btn danger sm" onclick={() => act(api('DELETE', `/api/sets/${st.id}`))}>Delete</button>
          </div>
        {/each}
        <button class="btn pri" onclick={() => (editSet = { name: '', client_id: null, jids: [] })}>New group set</button>
      {/if}
    {/if}
  </div>
</div>

<style>
  .scrim { position: fixed; inset: 0; background: rgba(17,27,33,.25); z-index: 470 }
  .modal { position: fixed; inset: 40px; max-width: 1000px; margin: 0 auto; background: var(--surface); border-radius: 14px; z-index: 480; display: grid; grid-template-columns: 190px 1fr; overflow: hidden; box-shadow: var(--shadow) }
  nav { background: #FBFAF8; border-right: 1px solid var(--line); padding: 14px 10px; display: flex; flex-direction: column; gap: 2px }
  nav b { font-size: 26px; margin: 0 8px 10px }
  nav button { border: 0; background: transparent; text-align: left; padding: 8px 10px; border-radius: 8px; color: var(--ink2) }
  nav button.on { background: var(--sunk); color: var(--ink); font-weight: 500 }
  .pane { overflow: auto; padding: 18px 22px; display: flex; flex-direction: column; gap: 8px }
  h3 { font: 600 20px/1 var(--display); text-transform: uppercase; color: var(--t800); margin: 10px 0 2px }
  .st { display: flex; align-items: center; gap: 8px; margin: 0 }
  .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--rose) } .dot.on { background: var(--grass) }
  .safe { display: flex; gap: 12px; align-items: center; justify-content: space-between; padding: 10px 12px; border-radius: 10px; background: var(--sunk) }
  .safe.on { background: var(--amber-bg); border: 1px solid var(--amber-line); color: var(--amber-ink) }
  .row { display: flex; gap: 8px; align-items: center; flex-wrap: wrap }
  .grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px 20px }
  .grid label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: var(--muted) }
  .grid span { display: flex; gap: 6px; align-items: center; color: var(--ink2) }
  .grid .inp { width: 90px }
  .ck { display: inline-flex; gap: 5px; align-items: center; font-size: 12.5px; white-space: nowrap }
  .ck input { accent-color: var(--t800) }
  .tbl { border: 1px solid var(--line2); border-radius: 10px; overflow: auto; flex: 1; min-height: 200px }
  .tr { display: grid; grid-template-columns: 22px 20px minmax(160px, 1fr) 150px 140px 76px; gap: 8px; align-items: center; padding: 5px 10px; border-bottom: 1px solid var(--line2) }
  .tr.off { opacity: .55 }
  .tr.th { position: sticky; top: 0; background: #FBFAF8; z-index: 1; padding-top: 4px; padding-bottom: 4px }
  .sorth { border: 0; background: transparent; padding: 3px 4px; border-radius: 5px; text-align: left; font: 500 10.5px var(--mono); letter-spacing: .08em; text-transform: uppercase; color: var(--muted); white-space: nowrap }
  .sorth:hover { background: var(--sunk); color: var(--ink) }
  .sortpair { display: flex; gap: 2px }
  .tr .av { border: 0; width: 20px; height: 20px }
  .nm { white-space: nowrap; overflow: hidden; text-overflow: ellipsis } .nm i { color: var(--muted); font-style: normal }
  .k { font-size: 11px; color: var(--muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis }
  .inp.sm { padding: 3px 6px; font-size: 12px }
  .star { border: 0; background: transparent; color: #D5D9D7; font-size: 14px; padding: 0 }
  .star.on { color: #D9962B }
  .named input[type=color] { width: 32px; height: 30px; border: 1px solid var(--line); border-radius: 6px; padding: 2px; background: #fff }
  .named .inp { max-width: 280px }
  code { font: 12px var(--mono); background: var(--sunk); padding: 2px 6px; border-radius: 5px }
  .links { margin: 4px 0 6px }
  .links a { text-decoration: none; color: var(--ink) }
  .secret { background: var(--amber-bg); border: 1px solid var(--amber-line); color: var(--amber-ink); border-radius: 10px; padding: 10px 12px; display: flex; flex-direction: column; gap: 8px }
  .sv { background: #fff; word-break: break-all; padding: 6px 8px } .sv.small { font-size: 11px; color: var(--ink2) }
  .scope { font: 500 11px var(--mono); background: #E3F2EE; color: var(--t800); border-radius: 4px; padding: 1px 6px }
  .key.revoked { opacity: .5 }
  .tgqr { width: 240px; height: 240px; image-rendering: pixelated; border-radius: 10px; border: 1px solid var(--line); background: #fff; padding: 8px }
  .err { color: #9B1C2C; background: #FDE4E7; padding: 6px 10px; border-radius: 8px; margin: 0 }
  .qseg { display: inline-flex; align-items: center; gap: 2px } .qseg .muted { margin-right: 4px; font-size: 12px }
  .qseg button { border: 1px solid var(--line); background: #fff; font-size: 12px; padding: 3px 8px; border-radius: 6px } .qseg button.on { background: var(--t800); border-color: var(--t800); color: #fff } .qt { width: 110px !important } .qz { max-width: 170px }
  @media (max-width: 920px) {
    .modal { inset: 0; border-radius: 0; grid-template-columns: 1fr; grid-template-rows: auto 1fr }
    nav { flex-direction: row; overflow-x: auto; border-right: 0; border-bottom: 1px solid var(--line); padding: 8px; align-items: center; gap: 4px }
    nav b { font-size: 22px; margin: 0 6px 0 4px }
    nav button { white-space: nowrap; padding: 8px 10px }
    .pane { padding: 14px }
    .grid { grid-template-columns: 1fr }
    .tr { grid-template-columns: 22px 20px minmax(0, 1fr) 70px; row-gap: 4px }
    .tr .k { grid-column: 3 / 5; grid-row: 2 }
    .tr select { grid-column: 3; grid-row: 3 }
    .tr .ck { grid-column: 4; grid-row: 3 }
    .tr.th .sortpair, .tr.th > :nth-child(5) { display: none }
    .tr.th > :nth-child(6) { grid-column: 4; grid-row: 1 }
    .safe { flex-direction: column; align-items: flex-start }
  }
</style>
