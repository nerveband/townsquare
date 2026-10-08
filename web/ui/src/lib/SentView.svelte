<script>
  // A post that already went out: what was sent, where, how it did, and what you
  // can still do about it: post it again (as a new draft), fix the text, or delete
  // it for everyone. Opening a sent post shows this instead of the editor.
  import { app, api, act, tz, toast, target, refresh } from './state.svelte.js'
  import { time12, prettyDay, dayKey, tzShort } from './time.js'
  import { openSend } from './peek.js'
  import WaPreview from './WaPreview.svelte'
  import Platform from './Platform.svelte'

  const s = $derived(app.sent)
  const p = $derived(s ? app.posts[s.post_id] : null)
  let eng = $state(null)
  let details = $state([])
  let mode = $state('') // '', 'edit', 'unsend'
  let check = $state(null) // dry-run result
  let busy = $state(false)
  let newText = $state('')
  let chats = $state([]) // chosen chats for edit/unsend
  let pv = $state('whatsapp')
  let showPeople = $state(false)

  const ref = $derived(s ? { post_id: s.post_id, schedule_id: s.schedule_id, occ: s.occ } : null)
  async function load() {
    if (!s) return
    eng = null
    details = await api('GET', `/api/sends/detail?schedule_id=${s.schedule_id}&occ=${encodeURIComponent(s.occ)}`).catch(() => [])
    eng = await api('GET', `/api/stats/posts/${s.post_id}?schedule_id=${s.schedule_id}&occ=${encodeURIComponent(s.occ)}`).catch(() => null)
  }
  $effect(() => {
    if (!s) return
    mode = ''; check = null; newText = s.caption; showPeople = false
    pv = s.targets.every((j) => j.startsWith('tg')) ? 'telegram' : 'whatsapp'
    load()
  })
  function close() { app.sent = null }
  const pc = (v) => `${Math.round((v || 0) * 100)}%`
  const statOf = (jid) => eng?.sends?.find((x) => x.chat === jid)
  const STATE = { sent: 'Sent', unsent: 'Deleted for everyone', blocked: 'Held', failed: 'Failed', missed: 'Missed' }
  const RESULT = { would_delete: 'Can be deleted', would_edit: 'Can be edited', deleted: 'Deleted', edited: 'Edited', already_deleted: 'Already deleted',
    too_old: 'Too old', cant_edit: "Can't edit", not_supported: 'Do it in Telegram', not_sent: "Didn't get it", failed: 'Failed' }
  const until = (u) => (u ? `until ${prettyDay(dayKey(new Date(u * 1000), tz()), false)} ${time12(new Date(u * 1000), tz())}` : 'no time limit')
  function curvePts(c) { const m = Math.max(1, ...c); return c.map((v, i) => `${(i / 47) * 100},${28 - (v / m) * 26}`).join(' ') }

  async function start(m) {
    mode = m; check = null; busy = true
    try {
      check = await api('POST', `/api/sends/${m === 'edit' ? 'edit' : 'unsend'}`, { ...ref, caption: m === 'edit' ? newText || s.caption : '', dry_run: true })
      chats = check.chats.filter((c) => c.result === 'would_delete' || c.result === 'would_edit').map((c) => c.jid)
    } catch (e) { toast(e.message); mode = '' } finally { busy = false }
  }
  async function apply() {
    busy = true
    try {
      const r = await api('POST', `/api/sends/${mode === 'edit' ? 'edit' : 'unsend'}`, { ...ref, caption: mode === 'edit' ? newText : '', chats })
      check = r
      const ok = r.chats.filter((c) => c.result === 'deleted' || c.result === 'edited').length
      const bad = r.chats.filter((c) => c.result === 'failed').length
      toast(mode === 'edit' ? `Edited in ${ok} chat${ok === 1 ? '' : 's'}${bad ? `, ${bad} failed` : ''}` : `Deleted for everyone in ${ok} chat${ok === 1 ? '' : 's'}${bad ? `, ${bad} failed` : ''}`)
      await refresh()
      await load()
    } catch (e) { toast(e.message) } finally { busy = false }
  }
  // Repost: a new draft with the same text and media, aimed at the same chats
  // (change them before scheduling). The sent post stays as it was.
  function repost() {
    const post = { title: p?.title ? `${p.title} (again)` : '', caption: s.caption, media: [...s.media], targets: [...s.targets],
      tag_id: p?.tag_id ?? null, client_id: p?.client_id ?? null, schedules: [] }
    app.sent = null
    app.composer = { post, scope: 'all', repost: true }
  }
  function editSeries() { const x = s; app.sent = null; openSend(x, 'future') }
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && s && close()} />
{#if s}
  <div class="scrim" onclick={close} role="presentation"></div>
  <aside class="drawer" aria-label="Sent post">
    <header>
      <div class="ttl">
        <b class="display">{p?.title || 'Sent post'}</b>
        <span class="muted">{s.delivery === 'unsent' ? 'Deleted for everyone' : 'Sent'} {prettyDay(dayKey(s.at, tz()))}, {time12(s.at, tz())} {tzShort(tz())}</span>
      </div>
      <button class="ib" onclick={close} aria-label="Close"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 6L6 18M6 6l12 12" /></svg></button>
    </header>

    <div class="scroll">
      <div class="acts">
        <button class="btn pri" onclick={repost}>Post again…</button>
        <button class="btn" class:on={mode === 'edit'} onclick={() => (mode === 'edit' ? (mode = '') : (mode = 'edit', check = null))}>Fix the text</button>
        <button class="btn danger" class:on={mode === 'unsend'} onclick={() => (mode === 'unsend' ? (mode = '') : start('unsend'))} disabled={busy}>Delete for everyone…</button>
        {#if s.repeating}<span class="sp"></span><button class="btn" onclick={editSeries}>Edit upcoming sends</button>{/if}
      </div>

      {#if mode === 'edit'}
        <section class="box">
          <p class="muted">Changes the text people already got. WhatsApp allows this for 15 minutes after sending, for text messages only (not photo captions). Telegram has no time limit. The post itself stays as it is.</p>
          <textarea class="inp" rows="5" bind:value={newText} aria-label="New text"></textarea>
          <div class="row"><button class="btn" onclick={() => start('edit')} disabled={busy || !newText.trim() || newText === s.caption}>Check which chats can be edited</button></div>
        </section>
      {/if}

      {#if check}
        <section class="box">
          <div class="chk">
            {#each check.chats as c (c.jid)}
              {@const ok = c.result === 'would_delete' || c.result === 'would_edit'}
              <label class="cr" class:dim={!ok}>
                {#if ok}<input type="checkbox" checked={chats.includes(c.jid)} onchange={(e) => (chats = e.target.checked ? [...chats, c.jid] : chats.filter((j) => j !== c.jid))} />{:else}<span class="nb"></span>{/if}
                <span class="nm">{#if c.platform?.startsWith('telegram')}<Platform platform="telegram" /> {/if}{c.name}</span>
                <span class="rs r-{c.result}">{RESULT[c.result] || c.result}</span>
                <small class="muted">{ok ? until(c.until) : c.note || ''}</small>
              </label>
            {/each}
          </div>
          {#if !check.dry_run}
            <p class="muted">Done. {mode === 'edit' ? 'Edits' : 'Deletions'} are recorded in History.</p>
          {:else if chats.length}
            <div class="row">
              <button class="btn {mode === 'unsend' ? 'danger solid' : 'pri'}" onclick={apply} disabled={busy}>
                {busy ? 'Working…' : mode === 'unsend' ? `Delete for everyone in ${chats.length} chat${chats.length === 1 ? '' : 's'}` : `Change the text in ${chats.length} chat${chats.length === 1 ? '' : 's'}`}
              </button>
              <span class="muted">{mode === 'unsend' ? 'People see “This message was deleted” on WhatsApp. This can’t be undone.' : 'WhatsApp marks the message “Edited”.'}</span>
            </div>
          {:else}
            <p class="muted">Nothing left to {mode === 'edit' ? 'edit' : 'delete'} here.</p>
          {/if}
        </section>
      {/if}

      <section>
        <h3>How it did</h3>
        {#if eng && eng.totals}
          <div class="tiles">
            <div><b>{eng.totals.reach ?? 0}</b><small>reached · {pc(eng.read_rate)}</small></div>
            <div><b>{eng.totals.reactions ?? 0}</b><small>reactions</small></div>
            <div><b>{eng.totals.replies ?? 0}</b><small>replies</small></div>
            <div><b>{eng.totals.forwards ?? 0}</b><small>shares</small></div>
          </div>
          {#if eng.curve?.some((v) => v > 0)}
            <svg class="curve" viewBox="0 0 100 30" preserveAspectRatio="none"><polyline points={curvePts(eng.curve)} fill="none" stroke="var(--sky)" stroke-width="1.5" vector-effect="non-scaling-stroke" /></svg>
            <div class="cx muted"><span>sent</span><span>reads over 48 hours</span><span>48h</span></div>
          {/if}
          {#if eng.emoji && Object.keys(eng.emoji).length}<div class="em">{#each Object.entries(eng.emoji).sort((a, b) => b[1] - a[1]) as [e, n]}<span>{e} {n}</span>{/each}</div>{/if}
          {#if eng.names && eng.people?.length}
            <button class="lnk" onclick={() => (showPeople = !showPeople)}>{showPeople ? 'Hide' : 'Who read it'} ({eng.people.length})</button>
            {#if showPeople}<div class="ppl">{#each eng.people as x}<div><span>{x.who || 'Someone'}</span><small class="muted">{x.what}{x.emoji ? ' ' + x.emoji : ''}</small></div>{/each}</div>{/if}
          {/if}
        {:else}
          <p class="muted">No numbers yet. Reads, views and reactions show up here as they come in.</p>
        {/if}
      </section>

      <section>
        <h3>Where it went</h3>
        <div class="dl">
          {#each details as d (d.jid)}
            {@const st = statOf(d.jid)}
            <div class="dr">
              <span class="nm">{#if d.jid.startsWith('tg')}<Platform platform="telegram" /> {/if}{target(d.jid)?.name || d.jid}</span>
              <span class="s s-{d.state}">{STATE[d.state] || d.state}</span>
              <span class="mono">{st ? `${st.reach} · ${pc(st.read_rate)} · ${st.reactions}♥` : ''}</span>
              {#if d.error && d.state !== 'sent'}<small class="muted er">{d.error}</small>{/if}
            </div>
          {/each}
        </div>
      </section>

      <section>
        <div class="pvh"><h3>What was sent</h3>
          <span class="pvseg"><button class:on={pv === 'whatsapp'} onclick={() => (pv = 'whatsapp')}>WhatsApp</button><button class:on={pv === 'telegram'} onclick={() => (pv = 'telegram')}>Telegram</button></span>
        </div>
        <div class="pvchat" class:tgbg={pv === 'telegram'}><WaPreview caption={s.caption} media={s.media} time={time12(s.at, tz())} platform={pv} /></div>
      </section>
    </div>
  </aside>
{/if}

<style>
  .scrim { position: fixed; inset: 0; background: rgba(17, 27, 33, .18); z-index: 450 }
  .drawer { position: fixed; top: 0; right: 0; bottom: 0; width: min(560px, 100vw); background: var(--surface); z-index: 460; display: flex; flex-direction: column; box-shadow: -24px 0 60px -30px rgba(6, 48, 43, .45); animation: slide .28s var(--ease) }
  @keyframes slide { from { transform: translateX(24px); opacity: 0 } }
  header { display: flex; align-items: center; gap: 10px; padding: 14px 16px; border-bottom: 1px solid var(--line) }
  .ttl { flex: 1; display: flex; flex-direction: column; gap: 3px; min-width: 0 }
  .ttl b { font-size: 24px; color: var(--t900); white-space: nowrap; overflow: hidden; text-overflow: ellipsis }
  .ttl .muted { font-size: 12px }
  .scroll { flex: 1; overflow: auto; padding: 14px 16px 30px; display: flex; flex-direction: column; gap: 18px }
  h3 { font: 600 17px/1 var(--display); text-transform: uppercase; color: var(--t800); margin: 0 0 8px }
  .acts { display: flex; flex-wrap: wrap; gap: 8px; align-items: center }
  .acts .sp { flex: 1 }
  .btn.on { border-color: var(--t600) }
  .btn.danger.solid { background: var(--rose); color: #fff; border: 0 }
  .box { border: 1px solid var(--line); border-radius: 10px; padding: 12px; display: flex; flex-direction: column; gap: 10px; background: #FBFAF8 }
  .box p { margin: 0; font-size: 12.5px }
  textarea.inp { resize: vertical; font: inherit; line-height: 1.45 }
  .row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap }
  .row .muted { font-size: 12px }
  .chk { display: flex; flex-direction: column; gap: 4px }
  .cr { display: grid; grid-template-columns: 18px 1fr auto; gap: 2px 8px; align-items: center; font-size: 13px; padding: 4px 0; border-bottom: 1px solid var(--line2) }
  .cr small { grid-column: 2 / 4; font-size: 11.5px }
  .cr.dim .nm { color: var(--muted) }
  .nb { width: 14px }
  .rs { font: 500 11px var(--mono); padding: 1px 6px; border-radius: 5px; background: var(--sunk); color: var(--ink2) }
  .r-would_delete, .r-would_edit, .r-deleted, .r-edited { background: #E9F7EE; color: #1E7A3A }
  .r-failed, .r-too_old { background: #FDE4E7; color: #9B1C2C }
  .tiles { display: grid; grid-template-columns: repeat(4, 1fr); gap: 8px }
  .tiles div { border: 1px solid var(--line); border-radius: 8px; padding: 8px 10px; display: flex; flex-direction: column }
  .tiles b { font: 600 24px/1 var(--display); color: var(--t900) }
  .tiles small { color: var(--muted); font-size: 11px; margin-top: 3px }
  .curve { width: 100%; height: 44px; margin-top: 10px; background: #F7FAFE; border-radius: 6px }
  .cx { display: flex; justify-content: space-between; font: 10.5px var(--mono) }
  .em { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px }
  .em span { background: var(--sunk); border-radius: 12px; padding: 2px 8px; font-size: 12.5px }
  .lnk { border: 0; background: none; padding: 0; color: var(--sky); font-weight: 500; margin-top: 8px }
  .ppl { display: flex; flex-direction: column; gap: 2px; margin-top: 6px; max-height: 220px; overflow: auto; font-size: 12.5px }
  .ppl div { display: flex; justify-content: space-between }
  .dl { display: flex; flex-direction: column }
  .dr { display: grid; grid-template-columns: 1fr auto auto; gap: 2px 10px; align-items: center; padding: 6px 0; border-bottom: 1px solid var(--line2); font-size: 13px }
  .dr .mono { font-size: 11.5px; color: var(--ink2) }
  .dr .er { grid-column: 1 / 4; font-size: 11.5px }
  .s { font-size: 11.5px; color: #1E7A3A }
  .s-blocked, .s-missed { color: #8A6A0B } .s-failed { color: #9B1C2C } .s-unsent { color: var(--muted) }
  .pvh { display: flex; align-items: center; gap: 10px; margin-bottom: 8px } .pvh h3 { margin: 0 }
  .pvseg { display: inline-flex; background: var(--sunk); border-radius: 7px; padding: 2px }
  .pvseg button { border: 0; background: transparent; border-radius: 5px; padding: 2px 9px; font-size: 12px; color: var(--ink2) }
  .pvseg button.on { background: var(--surface); color: var(--ink); box-shadow: 0 1px 2px rgba(0,0,0,.08); font-weight: 600 }
  .pvchat { background: var(--chat); border-radius: 10px; padding: 14px 12px }
  .pvchat.tgbg { background: linear-gradient(160deg, #D6E6C9 0%, #C2D9E6 55%, #B7CFE5 100%) }
  @media (max-width: 700px) { .tiles { grid-template-columns: repeat(2, 1fr) } }
</style>
