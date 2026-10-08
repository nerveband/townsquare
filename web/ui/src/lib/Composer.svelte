<script>
  import { app, api, act, tz, defaultTZ, avatar, target, toast, today, quietFor, accountLabel } from './state.svelte.js'
  import { localStr, dayKey, hhmm, prettyDay, time12, time12Str, parseRule, buildRule, ruleLabel, BYDAY, weekday, tzShort, fromLocal, zoneName } from './time.js'
  import TargetPicker from './TargetPicker.svelte'
  import NamedPicker from './NamedPicker.svelte'
  import WaPreview from './WaPreview.svelte'

  const c = app.composer
  const src = c.post
  const occ = c.occ
  const isNew = !src.id
  const repeating = !!(occ && occ.repeating)

  let scope = $state(c.scope || 'all')
  let title = $state(src.title || '')
  let caption = $state(occ ? occ.caption : src.caption || '')
  let media = $state([...(occ ? occ.media : src.media || [])])
  let targets = $state([...(occ ? occ.targets : src.targets || [])])
  let tagId = $state(src.tag_id ?? null)
  let clientId = $state(src.client_id ?? null)
  // One editable row per schedule. Times are edited in each schedule's own zone.
  let rows = $state(
    (src.schedules?.length ? src.schedules : [{ start: `${app.anchor >= today() ? app.anchor : today()}T09:00`, tz: tz(), rrule: '' }]).map((s) => {
      const r = parseRule(s.rrule)
      return { id: s.id || 0, date: s.start.slice(0, 10), time: s.start.slice(11, 16), tz: s.tz, mode: r.mode, days: r.days, raw: r.raw || s.rrule, until: (s.until || '').slice(0, 10), overrides: s.overrides || [] }
    })
  )
  // For "only this send": the send's current time, in the display zone.
  let oneDate = $state(occ ? dayKey(occ.at, tz()) : '')
  let oneTime = $state(occ ? hhmm(occ.at, tz()) : '')

  let picking = $state((isNew && !targets.length) || !!c.repost)
  let uploading = $state(0)
  let err = $state('')
  let preview = $state([])
  let dragOver = $state(false)
  let ta = $state()

  const seriesCount = $derived(occ ? app.sends.filter((x) => x.schedule_id === occ.schedule_id && x.at >= occ.at && x.delivery !== 'sent').length : 0)
  const isFirst = $derived(occ && occ.index === 0)

  function scheduleBody() {
    return rows.map((r) => ({
      id: r.id, start: `${r.date}T${r.time}`, tz: r.tz,
      rrule: buildRule(r.mode, r.days, r.date, r.raw),
      until: r.mode !== 'once' && r.until ? `${r.until}T23:59` : '',
      overrides: r.overrides,
    }))
  }

  // Next sends preview.
  let pt
  $effect(() => {
    const body = { schedules: scheduleBody(), caption, media, targets }
    if (scope === 'one') { preview = []; return }
    clearTimeout(pt)
    pt = setTimeout(async () => {
      try { preview = await api('POST', '/api/preview', body) } catch { preview = [] }
    }, 250)
  })

  // Quiet hours can differ per chat (client settings), each in its own zone.
  function quietAt(at, w) {
    if (!w.start || !w.end || w.start === w.end) return false
    const hm = hhmm(at, w.tz)
    return w.start < w.end ? hm >= w.start && hm < w.end : hm >= w.start || hm < w.end
  }
  const windows = $derived(targets.map((j) => ({ jid: j, ...quietFor(j, clientId) })))
  // Preview as WhatsApp or Telegram. Follows the chats picked until you choose.
  const hasTG = $derived(targets.some((j) => j.startsWith('tg')))
  const hasWA = $derived(targets.some((j) => !j.startsWith('tg')))
  let pvChoice = $state(localStorage.getItem('townsquare.preview') || '')
  const pvPlatform = $derived(pvChoice || (hasTG && !hasWA ? 'telegram' : 'whatsapp'))
  function setPv(p) { pvChoice = p; localStorage.setItem('townsquare.preview', p) }
  const inQuiet = (at) => windows.some((w) => quietAt(at, w))
  const quietHits = $derived.by(() => {
    const times = scope === 'one' ? (oneDate && oneTime ? [fromLocal(`${oneDate}T${oneTime}`, tz())] : []) : preview.map((p) => p.at)
    const hit = windows.filter((w) => times.some((t) => quietAt(t, w)))
    return hit.length ? hit : null
  })

  function close() { app.composer = null }

  function body(status) {
    return {
      ...src, title, caption, media, targets, tag_id: tagId, client_id: clientId,
      status, schedules: scheduleBody(),
      scope: repeating ? scope : 'all', schedule_id: occ?.schedule_id || 0, occ: occ?.occ || '',
      at: scope === 'one' ? `${oneDate}T${oneTime}` : '',
      at_tz: tz(),
    }
  }

  async function save(status) {
    err = ''
    if (status === 'scheduled' && !targets.length) { err = 'Pick at least one group, channel or chat.'; picking = true; return }
    try {
      if (isNew) await act(api('POST', '/api/posts', body(status)))
      else await act(api('PUT', `/api/posts/${src.id}`, body(status === 'keep' ? src.status : status)))
      close()
    } catch (e) { err = e.message }
  }

  async function testSend(platform) {
    try {
      await api('POST', '/api/test', { caption, media, platform })
      toast(platform === 'telegram' ? 'Sent a test to your Telegram Saved Messages' : 'Sent a test to your “Message yourself” chat')
    } catch (e) { err = e.message }
  }

  async function del() {
    try {
      if (repeating && scope !== 'all') await act(api('DELETE', `/api/posts/${src.id}?scope=${scope}&schedule_id=${occ.schedule_id}&occ=${encodeURIComponent(occ.occ)}`))
      else await act(api('DELETE', `/api/posts/${src.id}`))
      close()
    } catch (e) { err = e.message }
  }

  async function upload(files) {
    for (const f of files) {
      uploading++
      try {
        const fd = new FormData()
        fd.append('file', f)
        const m = await api('POST', '/api/media', fd)
        app.media[m.id] = m
        media = [...media, m.id]
      } catch (e) { err = e.message } finally { uploading-- }
    }
  }

  function wrap(mark) {
    const el = ta, a = el.selectionStart, b = el.selectionEnd
    const v = caption
    caption = v.slice(0, a) + mark + v.slice(a, b) + mark + v.slice(b)
    queueMicrotask(() => { el.focus(); el.setSelectionRange(a + mark.length, b + mark.length) })
  }

  // Grow the editor to fit long messages (the drawer scrolls instead of the box).
  function grow() {
    if (!ta) return
    ta.style.height = 'auto'
    ta.style.height = ta.scrollHeight + 2 + 'px'
  }
  $effect(() => { caption; queueMicrotask(grow) })

  // Prefix the selected lines (lists, quotes), toggling it off if every line has it.
  function prefix(p) {
    const el = ta, v = caption
    const a = v.lastIndexOf('\n', el.selectionStart - 1) + 1
    let b = v.indexOf('\n', el.selectionEnd)
    if (b < 0) b = v.length
    const lines = v.slice(a, b).split('\n')
    const all = lines.every((l) => l.startsWith(p) || (p === '1. ' && /^\d+\. /.test(l)))
    const out = lines.map((l, i) => (all ? l.replace(p === '1. ' ? /^\d+\. / : p, '') : (p === '1. ' ? `${i + 1}. ` : p) + l))
    caption = v.slice(0, a) + out.join('\n') + v.slice(b)
    queueMicrotask(() => el.focus())
  }

  let wide = $state(localStorage.getItem('townsquare.wideComposer') === '1')
  $effect(() => localStorage.setItem('townsquare.wideComposer', wide ? '1' : '0'))
  const previewTime = $derived(scope === 'one' ? time12Str(oneTime || '09:00') : rows[0] ? time12Str(rows[0].time || '09:00') : '')

  function addRow() {
    const last = rows[rows.length - 1]
    rows = [...rows, { id: 0, date: last?.date || today(), time: '09:00', tz: tz(), mode: 'once', days: [], raw: '', until: '', overrides: [] }]
  }

  const primaryLabel = $derived(
    isNew ? 'Schedule' :
    repeating ? (scope === 'one' ? 'Save this send' : scope === 'future' ? `Save ${seriesCount} ${seriesCount === 1 ? 'send' : 'sends'} from here` : 'Save whole series') :
    src.status === 'draft' ? 'Schedule' : 'Save changes'
  )
  // Same moment in the other zones you care about.
  function alsoIn(local, zone) {
    if (!local || local.length < 16) return []
    const at = fromLocal(local, zone)
    return [...new Set([zone, tz(), defaultTZ(), 'America/Los_Angeles', 'America/New_York'])]
      .filter((z) => z !== zone)
      .map((z) => `${time12(at, z)} ${tzShort(z)}${dayKey(at, z) !== local.slice(0, 10) ? ' (' + prettyDay(dayKey(at, z), false) + ')' : ''}`)
  }
  const mediaInfo = (id) => app.media[id] || { id, kind: 'image' }
  const dur = (s) => (s ? `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}` : '')
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && close()} />

<div class="scrim" onclick={close} role="presentation"></div>
<aside class="drawer" class:wide aria-label="Composer">
  <header>
    <input class="title" bind:value={title} placeholder={isNew ? 'Name this post (optional)' : 'Untitled post'} aria-label="Post name" />
    <NamedPicker kind="tag" bind:value={tagId} />
    <NamedPicker kind="client" bind:value={clientId} />
    <button class="ib wideb" onclick={() => (wide = !wide)} title={wide ? 'Narrow editor' : 'Wide editor with preview beside it'} aria-label="Toggle wide editor">
      {#if wide}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 14h6v6M20 10h-6V4M14 10l7-7M3 21l7-7" /></svg>
      {:else}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M15 3h6v6M9 21H3v-6M21 3l-7 7M3 21l7-7" /></svg>{/if}
    </button>
    <button class="ib" onclick={close} aria-label="Close"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 6L6 18M6 6l12 12" /></svg></button>
  </header>

  {#if repeating}
    <div class="recur">
      <div class="rh"><span class="ri">↻</span><span><b>Repeating post</b> ({ruleLabel(occ.rrule)}). You opened the <b>{prettyDay(dayKey(occ.at, tz()))}</b> send.</span></div>
      <div class="scope" role="radiogroup" aria-label="Apply changes to">
        <button class:on={scope === 'one'} role="radio" aria-checked={scope === 'one'} onclick={() => (scope = 'one')}>Only this send<small>{prettyDay(dayKey(occ.at, tz()))}</small></button>
        {#if !isFirst}
          <button class:on={scope === 'future'} role="radio" aria-checked={scope === 'future'} onclick={() => (scope = 'future')}>This and later<small>{seriesCount}+ sends from here</small></button>
        {/if}
        <button class:on={scope === 'all'} role="radio" aria-checked={scope === 'all'} onclick={() => (scope = 'all')}>Whole series<small>past sends stay as sent</small></button>
      </div>
    </div>
  {/if}

  <div class="scroll">
    {#if c.repost}<p class="repost">A copy of a post that already went out. Pick the chats (the same ones are ticked) and a time, then schedule it. The original stays as it was.</p>{/if}
    <section class="to">
      <span class="lbl">To</span>
      <div class="chips">
        {#each targets as j (j)}
          {@const a = avatar(j)}
          <span class="chip"><span class="av" style="background:{a.bg}">{a.ini}</span><span>{target(j).name}{#if accountLabel(j)}<small class="acc"> · {accountLabel(j)}</small>{/if}</span><button aria-label="Remove {target(j).name}" onclick={() => (targets = targets.filter((x) => x !== j))}>×</button></span>
        {/each}
        <button class="add" onclick={() => (picking = !picking)}>{picking ? 'Done' : '+ Add'}</button>
      </div>
      {#if picking}<div class="pick"><TargetPicker bind:selected={targets} /></div>{/if}
    </section>

    <section class="msg" class:over={dragOver}
      ondragover={(e) => { if (e.dataTransfer.types.includes('Files')) { e.preventDefault(); dragOver = true } }}
      ondragleave={() => (dragOver = false)}
      ondrop={(e) => { e.preventDefault(); dragOver = false; upload([...e.dataTransfer.files]) }}
      aria-label="Message">
      <div class="mh"><span class="lbl">Message</span><span class="cnt">{caption.length.toLocaleString()} characters</span></div>
      {#if media.length || uploading}
        <div class="media">
          {#each media as id, i (id)}
            {@const m = mediaInfo(id)}
            <div class="m">
              {#if m.kind === 'image' || m.kind === 'video'}<img src="/api/media/{id}/preview" alt={m.name} onerror={(e) => e.currentTarget.classList.add('broken')} />
              {:else if m.kind === 'voice' || m.kind === 'audio'}<div class="th-voice">{#each [10, 22, 14, 30, 12, 24, 8, 18] as h}<i style="height:{h}px"></i>{/each}</div>
              {:else}<div class="th-doc">{(m.name || '').split('.').pop()}</div>{/if}
              <span class="badge" style="left:4px;bottom:4px">{m.kind === 'video' ? dur(m.seconds) : m.kind === 'voice' ? 'VOICE ' + dur(m.seconds) : m.kind.toUpperCase()}</span>
              <button class="x" aria-label="Remove" onclick={() => (media = media.filter((_, k) => k !== i))}>×</button>
            </div>
          {/each}
          {#if uploading}<div class="m loading">Converting…</div>{/if}
        </div>
      {/if}
      <div class="editor">
        <div class="tools">
          <button title="Bold (*text*)" onclick={() => wrap('*')}><b>B</b></button>
          <button title="Italic (_text_)" onclick={() => wrap('_')}><i>I</i></button>
          <button title="Strikethrough (~text~)" onclick={() => wrap('~')}><s>S</s></button>
          <button title="Monospace (```text```)" onclick={() => wrap('```')}>{'{}'}</button>
          <button title="Bulleted list" onclick={() => prefix('- ')}>•</button>
          <button title="Numbered list" onclick={() => prefix('1. ')}>1.</button>
          <button title="Quote" onclick={() => prefix('> ')}>❝</button>
          <label class="attach">＋ Photo, video, voice, file<input type="file" multiple onchange={(e) => { upload([...e.target.files]); e.target.value = '' }} /></label>
        </div>
        <textarea bind:this={ta} bind:value={caption} placeholder={media.length ? 'Add a caption' : 'Type your message'} rows="5" oninput={grow}></textarea>
      </div>
      {#if media.length > 1}<p class="hint">Each item goes as its own message; the caption rides on the first one.</p>{/if}
    </section>

    <section class="pv">
      <div class="pvh"><span class="lbl">Preview</span>
        <span class="pvseg" role="radiogroup" aria-label="Preview as">
          <button class:on={pvPlatform === 'whatsapp'} onclick={() => setPv('whatsapp')}>WhatsApp</button>
          <button class:on={pvPlatform === 'telegram'} onclick={() => setPv('telegram')}>Telegram</button>
        </span>
        {#if hasWA && hasTG}<span class="muted">this post goes to both</span>{/if}
      </div>
      <div class="pvchat" class:tgbg={pvPlatform === 'telegram'}><WaPreview {caption} {media} time={previewTime} platform={pvPlatform} /></div>
    </section>

    <section class="when">
      <span class="lbl">When {#if scope !== 'one' && rows.some((r) => r.tz !== tz())}<i class="muted">(times in each row's zone)</i>{/if}</span>
      {#if scope === 'one'}
        <div class="row">
          <input class="inp" type="date" bind:value={oneDate} />
          <input class="inp" type="time" bind:value={oneTime} />
          <span class="muted">{tzShort(tz())} · this send only</span>
        </div>
        <p class="also">Same moment: {alsoIn(`${oneDate}T${oneTime}`, tz()).join(' · ')}</p>
      {:else}
        {#each rows as r, i (i)}
          <div class="row">
            <input class="inp" type="date" bind:value={r.date} aria-label="Date" />
            <input class="inp" type="time" bind:value={r.time} aria-label="Time" />
            <select class="inp" bind:value={r.mode} aria-label="Repeat">
              <option value="once">Once</option><option value="daily">Every day</option><option value="weekdays">Weekdays</option>
              <option value="weekly">Weekly</option><option value="monthly">Monthly</option><option value="custom">Custom rule</option>
            </select>
            <span class="zone" title={r.tz}>{tzShort(r.tz)}</span>
            {#if rows.length > 1}<button class="ib" aria-label="Remove time" onclick={() => (rows = rows.filter((_, k) => k !== i))}>×</button>{/if}
          </div>
          <p class="also">{zoneName(r.tz)} time. Same moment: {alsoIn(`${r.date}T${r.time}`, r.tz).join(' · ')}</p>
          {#if r.mode === 'weekly'}
            <div class="days">
              {#each BYDAY as d, k}
                {@const on = r.days.length ? r.days.includes(d) : k === weekday(r.date)}
                <button class:on onclick={() => { const cur = r.days.length ? r.days : [BYDAY[weekday(r.date)]]; r.days = on ? cur.filter((x) => x !== d) : [...cur, d] }}>{d[0]}</button>
              {/each}
            </div>
          {/if}
          {#if r.mode === 'custom'}<input class="inp mono" bind:value={r.raw} placeholder="FREQ=WEEKLY;INTERVAL=2;BYDAY=FR" />{/if}
          {#if r.mode !== 'once'}
            <div class="ends"><span class="muted">Ends</span><input class="inp" type="date" bind:value={r.until} aria-label="Ends on" /><span class="muted">{r.until ? '' : 'never'}</span></div>
          {/if}
        {/each}
        <button class="addw" onclick={addRow}>+ Add another time</button>
        {#if preview.length}
          <div class="next">
            <span class="muted">Next ({tzShort(tz())}):</span>
            {#each preview.slice(0, 6) as p}<span class:q={inQuiet(p.at)}>{prettyDay(dayKey(p.at, tz()), false)} {time12(p.at, tz(), true)}</span>{/each}
          </div>
        {/if}
      {/if}
      {#if targets.some((j) => j.startsWith('tg:story:')) && !media.some((id) => ['image', 'video'].includes(mediaInfo(id).kind))}<p class="warn">Telegram stories need a photo or video. Add one, or remove the story target.</p>{/if}
      {#if quietHits}<p class="warn">Inside quiet hours for {quietHits.map((w) => `${target(w.jid).name} (${w.start} to ${w.end} ${tzShort(w.tz)}${w.client ? ', ' + w.client : ''})`).join(', ')}. Those sends will <b>not</b> go out. Change the time, or the quiet hours in Settings (globally or per client).</p>{/if}
    </section>
    {#if err}<p class="err">{err}</p>{/if}
  </div>

  <footer>
    <button class="btn" onclick={() => testSend('whatsapp')} disabled={!caption.trim() && !media.length}>Test on WhatsApp</button>
    {#if app.telegram === 'ready'}<button class="btn" onclick={() => testSend('telegram')} disabled={!caption.trim() && !media.length}>Test on Telegram</button>{/if}
    {#if !isNew}<button class="btn danger" onclick={del}>{repeating && scope === 'one' ? 'Skip this send' : repeating && scope === 'future' ? 'Stop from here' : 'Delete'}</button>{/if}
    <span style="flex:1"></span>
    {#if isNew || src.status === 'draft'}<button class="btn" onclick={() => save('draft')}>Save draft</button>{/if}
    <button class="btn pri" onclick={() => save(isNew || src.status === 'draft' ? 'scheduled' : 'keep')}>{primaryLabel}</button>
  </footer>
</aside>

<style>
  .scrim { position: fixed; inset: 0; background: rgba(17, 27, 33, .18); z-index: 450 }
  .drawer { position: fixed; top: 0; right: 0; bottom: 0; width: min(560px, 100vw); background: var(--surface); z-index: 460; display: flex; flex-direction: column; box-shadow: -24px 0 60px -30px rgba(6, 48, 43, .45); animation: slide .28s var(--ease) }
  @keyframes slide { from { transform: translateX(30px); opacity: 0 } }
  header { display: flex; align-items: center; gap: 6px; padding: 10px 12px 10px 16px; border-bottom: 1px solid var(--line2) }
  .title { border: 0; font: 600 24px/1 var(--display); text-transform: uppercase; flex: 1; outline: none; min-width: 0; background: transparent }
  .title::placeholder { color: #B9BFC2 }
  .pill { border: 1px solid var(--line); border-radius: 7px; padding: 4px 6px; font-size: 12px; background: #fff; max-width: 120px }
  .recur { background: var(--amber-bg); border-bottom: 1px solid var(--amber-line); padding: 10px 16px }
  .rh { display: flex; align-items: center; gap: 8px; color: var(--amber-ink) }
  .rh b { font-weight: 600 }
  .ri { width: 22px; height: 22px; border-radius: 50%; background: #F5DFA8; display: grid; place-items: center; flex: none }
  .scope { display: flex; gap: 4px; margin-top: 8px; background: #F7EBCD; border-radius: 9px; padding: 3px }
  .scope button { flex: 1; border: 0; background: transparent; border-radius: 7px; padding: 6px; font-size: 12px; font-weight: 500; color: #6B4D12; line-height: 1.25 }
  .scope button small { display: block; font-weight: 400; font-size: 10.5px; opacity: .8 }
  .scope button.on { background: #fff; color: var(--ink); box-shadow: 0 1px 2px rgba(0, 0, 0, .1) }
  .scroll { flex: 1; overflow: auto }
  section { padding: 10px 16px }
  .lbl { display: block; font-size: 12px; color: var(--muted); margin-bottom: 6px }
  .to { border-bottom: 1px solid var(--line2) }
  .chips { display: flex; flex-wrap: wrap; gap: 6px; align-items: center }
  .chip { font-size: 12.5px; padding: 3px 6px 3px 3px }
  .chip .av { width: 20px; height: 20px; font-size: 8.5px }
  .chip button { border: 0; background: transparent; color: var(--muted); padding: 0 2px; font-size: 14px; line-height: 1 }
  .chip button:hover { color: var(--rose) }
  .add { border: 1px dashed #CFCAC0; background: transparent; border-radius: 99px; padding: 3px 10px; font-size: 12.5px; color: var(--ink2) }
  .add:hover { border-color: var(--t600); color: var(--t600) }
  .pick { margin-top: 8px }
  .msg { transition: box-shadow .15s }
  .msg.over { box-shadow: inset 0 0 0 2px var(--sky) }
  .mh { display: flex; justify-content: space-between; align-items: baseline }
  .cnt { font: 400 11px var(--mono); color: var(--muted) }
  .media { display: grid; grid-template-columns: repeat(auto-fill, minmax(72px, 1fr)); gap: 6px; margin-bottom: 8px }
  .m { position: relative; aspect-ratio: 1; background: var(--sunk); display: flex; border-radius: 8px; overflow: hidden }
  .m img { width: 100%; height: 100%; object-fit: cover }
  .m.loading { display: grid; place-items: center; font-size: 11px; color: var(--muted) }
  .m .x { position: absolute; top: 4px; right: 4px; width: 20px; height: 20px; border-radius: 50%; border: 0; background: rgba(17, 27, 33, .55); color: #fff; font-size: 12px; line-height: 1; opacity: 0; transition: opacity .15s }
  .m:hover .x, .m .x:focus-visible { opacity: 1 }
  .editor { border: 1px solid var(--line); border-radius: 10px; background: #fff; transition: border-color .15s, box-shadow .15s }
  .editor:focus-within { border-color: var(--t600); box-shadow: 0 0 0 3px rgba(18, 140, 126, .12) }
  .tools { display: flex; gap: 1px; align-items: center; flex-wrap: wrap; padding: 4px 6px; border-bottom: 1px solid var(--line2) }
  .tools button { border: 0; background: transparent; border-radius: 5px; padding: 3px 8px; font: 600 12.5px var(--mono); color: var(--t800); min-width: 28px }
  .tools button:hover, .attach:hover { background: rgba(7, 94, 84, .1) }
  .attach { margin-left: auto; font-size: 12px; font-weight: 500; color: var(--t800); padding: 3px 7px; border-radius: 5px; cursor: pointer }
  .attach input { display: none }
  textarea { display: block; width: 100%; border: 0; background: transparent; resize: none; outline: none; padding: 10px 12px; font-size: 14px; line-height: 1.5; min-height: 110px; overflow: hidden }
  .pv { padding-top: 4px }
  .pvh { display: flex; gap: 8px; align-items: baseline; margin-bottom: 6px } .pvh .lbl { margin: 0 } .pvh .muted { font-size: 11.5px }
  .pvchat { background: var(--chat); border-radius: 10px; padding: 14px 12px; background-image: radial-gradient(rgba(0,0,0,.035) 1px, transparent 1px); background-size: 14px 14px }
  .drawer.wide { width: min(1120px, 100vw) }
  .wide .scroll { display: grid; grid-template-columns: minmax(0, 1fr) minmax(320px, 440px); align-content: start; column-gap: 4px }
  .wide .scroll > section, .wide .scroll > .err { grid-column: 1 }
  .wide .scroll > .pv { grid-column: 2; grid-row: 1 / span 6; position: sticky; top: 0; align-self: start; max-height: 100%; overflow: auto; border-left: 1px solid var(--line2); padding-top: 10px }
  @media (max-width: 900px) { .wideb { display: none } .drawer.wide { width: min(560px, 100vw) } .wide .scroll { display: block } .wide .scroll > .pv { position: static; border: 0 } }
  .hint { margin: 0; font-size: 11.5px; color: #5D6E68 }
  .when .row { display: flex; gap: 6px; align-items: center; margin-bottom: 6px }
  .when .row .inp { width: auto }
  .also { margin: -2px 0 8px; font-size: 11.5px; color: #1B6A93 }
  .zone { font: 500 11px var(--mono); color: var(--muted) }
  .days { display: flex; gap: 4px; margin: 0 0 8px }
  .days button { width: 28px; height: 28px; border-radius: 50%; border: 1px solid var(--line); background: #fff; font-size: 12px; font-weight: 600 }
  .days button.on { background: var(--sky); border-color: var(--sky); color: #fff }
  .ends { display: flex; gap: 6px; align-items: center; margin-bottom: 8px }
  .ends .inp { width: auto }
  .addw { border: 0; background: transparent; color: var(--t600); font-weight: 500; padding: 4px 0 }
  .next { display: flex; flex-wrap: wrap; gap: 4px; align-items: center; margin-top: 6px; font-size: 11px }
  .next span:not(.muted) { font: 500 10.5px var(--mono); padding: 2px 6px; border-radius: 4px; background: var(--sunk) }
  .next span.q { background: #FBF0D9; color: #865A07 }
  .warn { margin: 8px 0 0; font-size: 12px; color: #865A07; background: #FBF0D9; padding: 6px 8px; border-radius: 6px }
  .err { margin: 0 16px 10px; color: #9B1C2C; background: #FDE4E7; padding: 8px 10px; border-radius: 8px }
  footer { display: flex; align-items: center; gap: 8px; padding: 10px 16px; border-top: 1px solid var(--line2); background: #FBFAF8; flex-wrap: wrap }
  @media (max-width: 700px) {
    header { flex-wrap: wrap; padding: 8px 10px }
    .title { flex-basis: calc(100% - 44px); font-size: 22px }

    section, .recur { padding-left: 12px; padding-right: 12px }
    .scope button { padding: 8px 4px }
    .when .row { flex-wrap: wrap }
    .when .row .inp { flex: 1; min-width: 120px; padding: 9px 10px }
    footer { padding: 10px 12px calc(10px + env(safe-area-inset-bottom)) }
    footer .btn { padding: 10px 12px }
    footer .btn.pri { flex: 1; white-space: nowrap }
    footer .btn { white-space: nowrap }
    textarea { font-size: 16px }
  }
  .chip .acc { color: var(--t800); font-weight: 600 }
  .repost { margin: 0 0 10px; background: var(--note); border-radius: 8px; padding: 8px 10px; font-size: 12.5px; color: var(--t900) }
  .pvseg { display: inline-flex; background: var(--sunk); border-radius: 7px; padding: 2px; margin-left: 4px }
  .pvseg button { border: 0; background: transparent; border-radius: 5px; padding: 2px 9px; font-size: 12px; color: var(--ink2) }
  .pvseg button.on { background: var(--surface); color: var(--ink); box-shadow: 0 1px 2px rgba(0,0,0,.08); font-weight: 600 }
  .pvchat.tgbg { background: #C9DCE8 linear-gradient(160deg, #D6E6C9 0%, #C2D9E6 55%, #B7CFE5 100%) }
</style>
