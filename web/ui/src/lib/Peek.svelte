<script>
  import { waHTML, waPlain } from './wafmt.js'
  import { app, api, act, tz, target, tagOf, clientOf } from './state.svelte.js'
  import { time12, prettyDay, dayKey, ruleLabel, tzShort } from './time.js'
  import { holdPeek, closePeek, openSend } from './peek.js'
  import Thumb from './Thumb.svelte'
  import TargetPicker from './TargetPicker.svelte'

  const pk = $derived(app.peek)
  const s = $derived(pk?.s)
  const p = $derived(s ? app.posts[s.post_id] : null)
  let picking = $state(false)
  let sel = $state([])
  let details = $state([])
  let eng = $state(null)
  let showPeople = $state(false)
  const pc = (v) => `${Math.round((v || 0) * 100)}%`
  const WHAT = { read: 'read', played: 'played', react: 'reacted', reply: 'replied' }
  function curvePts(c) { const m = Math.max(1, ...c); return c.map((v, i) => `${(i / 47) * 100},${28 - (v / m) * 26}`).join(' ') }
  let el = $state()
  let pos = $state({ x: 0, y: 0 })

  $effect(() => {
    if (!s) { picking = false; return }
    sel = [...s.targets]
    details = []
    eng = null
    showPeople = false
    if (s.delivery) api('GET', `/api/sends/detail?schedule_id=${s.schedule_id}&occ=${encodeURIComponent(s.occ)}`).then((d) => (details = d))
    if (s.delivery === 'sent' || s.delivery === 'partial') api('GET', `/api/stats/posts/${s.post_id}?schedule_id=${s.schedule_id}&occ=${encodeURIComponent(s.occ)}`).then((d) => (eng = d.sends?.length ? d : null)).catch(() => {})
  })
  $effect(() => {
    if (!pk || !el) return
    const r = pk.rect, w = 320, h = el.offsetHeight || 360
    let x = r.right + 8
    if (x + w > innerWidth - 8) x = r.left - w - 8
    if (x < 8) x = 8
    let y = r.top - 10
    if (y + h > innerHeight - 8) y = innerHeight - h - 8
    pos = { x, y: Math.max(8, y) }
  })

  const series = $derived(s ? app.sends.filter((x) => x.schedule_id === s.schedule_id && x.at >= s.at).slice(0, 5) : [])
  const clean = (t) => (t || '').replace(/[*_~`]/g, '')
  const changed = $derived(s && (sel.length !== s.targets.length || sel.some((j) => !s.targets.includes(j))))

  async function saveTargets(scope) {
    const body = { ...p, targets: sel, scope, schedule_id: s.schedule_id, occ: s.occ }
    if (scope === 'one') body.caption = s.caption, body.media = s.media
    app.peek = null
    await act(api('PUT', `/api/posts/${p.id}`, body))
  }
  async function sendNow() { app.peek = null; await act(api('POST', `/api/posts/${p.id}/send-now`), `Sending ${p.title || 'post'} now`) }
  async function skip() { app.peek = null; await act(api('POST', '/api/sends/skip', { post_id: p.id, schedule_id: s.schedule_id, occ: s.occ })) }
  async function del() { app.peek = null; await act(api('DELETE', `/api/posts/${p.id}`)) }
  async function pause() { app.peek = null; await act(api('PUT', `/api/posts/${p.id}`, { ...p, status: p.status === 'paused' ? 'scheduled' : 'paused' })) }
</script>

{#if s && p}
  {#if pk.sheet}<div class="scrim" role="presentation" onclick={() => (app.peek = null)}></div>{/if}
  <div class="peek" class:sheet={pk.sheet} bind:this={el} style={pk.sheet ? '' : `left:${pos.x}px;top:${pos.y}px`} role="dialog" aria-label="Post preview" tabindex="-1"
    onmouseenter={() => { holdPeek(); app.peekPinned = true }} onmouseleave={() => { app.peekPinned = false; if (!picking) closePeek() }}>
    {#if s.media.length}<div class="th"><Thumb media={s.media} caption={s.caption} big /></div>{/if}
    <div class="bd">
      <div class="hd">
        <h6>{p.title || waPlain(s.caption).split('\n')[0] || 'Untitled'}</h6>
        {#if tagOf(p.tag_id)}<span class="tag" style="--c:{tagOf(p.tag_id).color}">{tagOf(p.tag_id).name}</span>{/if}
        {#if clientOf(p.client_id)}<span class="tag" style="--c:{clientOf(p.client_id).color}">{clientOf(p.client_id).name}</span>{/if}
      </div>
      {#if s.caption}<div class="cap wa" class:bubble={!s.media.length}>{@html waHTML(s.caption)}</div>{/if}
      <div class="when">
        <b>{prettyDay(dayKey(s.at, tz()))}, {time12(s.at, tz())} {tzShort(tz())}</b>
        {#if s.tz !== tz()}<div class="alt">Scheduled as {time12(s.at, s.tz)} {tzShort(s.tz)} ({s.tz.split('/').pop().replace('_', ' ')})</div>{/if}
        {#if s.repeating}
          <div class="rep">↻ {ruleLabel(s.rrule)} · send #{s.index + 1}{s.edited ? ' · edited separately ✎' : ''}</div>
          <div class="times">{#each series as x (x.occ)}<span class:cur={x.occ === s.occ}>{prettyDay(dayKey(x.at, tz()), false)}</span>{/each}</div>
        {/if}
      </div>
      <div class="tg">
        {#if !picking}
          <div class="chips">{#each s.targets as j (j)}<span class="chip">{target(j).name}</span>{/each}</div>
          <button class="lnk" onclick={() => (picking = true)}>Change</button>
        {:else}
          <TargetPicker bind:selected={sel} compact />
          <div class="pa">
            {#if s.repeating}
              <button class="btn sm" disabled={!changed} onclick={() => saveTargets('one')}>Only this send</button>
              <button class="btn sm pri" disabled={!changed} onclick={() => saveTargets('all')}>Whole series</button>
            {:else}
              <button class="btn sm pri" disabled={!changed} onclick={() => saveTargets('all')}>Save groups</button>
            {/if}
            <button class="btn sm" onclick={() => { picking = false; sel = [...s.targets] }}>Cancel</button>
          </div>
        {/if}
      </div>
      {#if eng}
        <div class="eng">
          <div class="en">
            <div><b>{eng.totals.reach.toLocaleString()}</b><span>reached{eng.totals.members ? ` · ${pc(eng.read_rate)}` : ''}</span></div>
            <div><b>{eng.totals.reactions}</b><span>reactions</span></div>
            <div><b>{eng.totals.replies}</b><span>replies</span></div>
            {#if eng.totals.forwards}<div><b>{eng.totals.forwards}</b><span>shares</span></div>{/if}
          </div>
          {#if eng.curve.some((v) => v)}
            <svg viewBox="0 0 100 30" preserveAspectRatio="none" class="cv"><polyline points={curvePts(eng.curve)} fill="none" stroke="#1673E6" stroke-width="1.6" vector-effect="non-scaling-stroke" /></svg>
            <div class="cvl"><span>sent</span><span>reads over 48 hours</span><span>48h</span></div>
          {/if}
          {#if Object.keys(eng.emoji).length}<div class="emo">{#each Object.entries(eng.emoji).sort((a, b) => b[1] - a[1]) as [e, n]}<span>{e} {n}</span>{/each}</div>{/if}
          {#if eng.sends.length > 1}
            <div class="per">{#each eng.sends as x}<div><span>{x.name}</span><i>{x.reach.toLocaleString()}{x.members ? ` · ${pc(x.read_rate)}` : ''}{x.reactions ? ` · ${x.reactions}♥` : ''}</i></div>{/each}</div>
          {/if}
          {#if eng.names && eng.people?.length}
            <button class="lnk" onclick={() => (showPeople = !showPeople)}>{showPeople ? 'Hide' : 'Who read it'} ({eng.people.length})</button>
            {#if showPeople}<div class="ppl">{#each eng.people as p}<div><span>{p.who}</span><i>{WHAT[p.what] || p.what}{p.emoji ? ' ' + p.emoji : ''}</i></div>{/each}</div>{/if}
          {/if}
        </div>
      {/if}
      {#if details.length}
        <div class="det">
          {#each details as d}<div class={d.state}><span>{target(d.jid).name}</span><i>{d.state}{d.error ? ': ' + d.error : ''}</i></div>{/each}
        </div>
      {/if}
      {#if s.repeating}<p class="note">Part of a repeating post. Edits ask whether they apply to this send or the series.</p>{/if}
    </div>
    <div class="acts">
      {#if s.repeating}
        <button onclick={() => openSend(s, 'one')}>Edit this send</button>
        <button onclick={() => openSend(s, 'all')}>Edit series</button>
        <button onclick={skip}>Skip</button>
      {:else}
        <button onclick={() => openSend(s, 'all')}>Edit</button>
        <button onclick={del}>Delete</button>
      {/if}
      <button onclick={pause}>{p.status === 'paused' ? 'Resume' : 'Pause'}</button>
      <button onclick={sendNow}>Send now</button>
    </div>
  </div>
{/if}

<style>
  .eng { border-top: 1px solid var(--line2); padding-top: 7px; display: flex; flex-direction: column; gap: 6px }
  .en { display: flex; gap: 12px; flex-wrap: wrap }
  .en div { display: flex; flex-direction: column }
  .en b { font: 600 20px/1 var(--display); color: var(--t900) }
  .en span { font-size: 10.5px; color: var(--muted) }
  .cv { width: 100%; height: 30px; display: block; background: var(--sky-soft); border-radius: 6px }
  .cvl { display: flex; justify-content: space-between; font: 9.5px var(--mono); color: var(--muted); margin-top: -3px }
  .emo { display: flex; flex-wrap: wrap; gap: 4px }
  .emo span { background: var(--sunk); border-radius: 99px; padding: 1px 7px; font-size: 11.5px }
  .per div, .ppl div { display: flex; gap: 6px; justify-content: space-between; font-size: 11.5px }
  .per i, .ppl i { font-style: normal; color: var(--muted); font-family: var(--mono); font-size: 11px }
  .ppl { max-height: 160px; overflow: auto }
  .peek { position: fixed; z-index: 400; width: 320px; max-height: calc(100vh - 16px); overflow: auto; background: var(--surface); border: 1px solid var(--line); border-radius: 12px; box-shadow: var(--shadow); font-size: 12.5px }
  .th { height: 150px; border-radius: 11px 11px 0 0; overflow: hidden; display: flex }
  .bd { padding: 10px 12px; display: flex; flex-direction: column; gap: 8px }
  .hd { display: flex; align-items: center; gap: 6px; flex-wrap: wrap }
  h6 { margin: 0; font-size: 14px; font-weight: 600; margin-right: auto }
  .tag { font-size: 10.5px; padding: 1px 6px; border-radius: 4px; background: color-mix(in srgb, var(--c) 15%, #fff); color: color-mix(in srgb, var(--c) 70%, #000) }
  .cap { color: var(--ink2); max-height: 260px; overflow: auto; font-size: 13px }
  .cap.bubble { background: var(--note); color: var(--ink); padding: 6px 8px; border-radius: 8px 2px 8px 8px }
  .when b { font-weight: 600 }
  .rep { color: var(--t800); margin-top: 2px }
  .alt { color: #1B6A93; font-size: 12px; margin-top: 2px }
  .times { display: flex; flex-wrap: wrap; gap: 4px; margin-top: 5px }
  .times span { font: 500 10.5px var(--mono); padding: 2px 6px; border-radius: 4px; background: var(--sunk) }
  .times span.cur { background: var(--note); color: var(--t800) }
  .tg { display: flex; flex-direction: column; gap: 6px }
  .chips { display: flex; flex-wrap: wrap; gap: 4px }
  .chip { padding: 2px 8px }
  .lnk { align-self: flex-start; border: 0; background: transparent; color: var(--t600); font-weight: 500; padding: 2px 0 }
  .pa { display: flex; gap: 6px; flex-wrap: wrap }
  .det { font-size: 11.5px; border-top: 1px solid var(--line2); padding-top: 6px }
  .det div { display: flex; gap: 6px; justify-content: space-between }
  .det i { font-style: normal; color: var(--muted); text-align: right }
  .det .failed i, .det .missed i { color: var(--rose) }
  .det .blocked i { color: #865A07 }
  .note { margin: 0; color: #865A07; font-size: 11.5px }
  .acts { display: flex; flex-wrap: wrap; gap: 2px; border-top: 1px solid var(--line2); padding: 6px 8px }
  .acts button { border: 0; background: transparent; font-size: 12px; padding: 5px 8px; border-radius: 6px; color: var(--ink2) }
  .acts button:hover { background: var(--sunk); color: var(--ink) }
  .scrim { position: fixed; inset: 0; background: rgba(17,27,33,.25); z-index: 399 }
  .peek.sheet { left: 0; right: 0; bottom: 0; top: auto; width: auto; max-height: 82vh; border-radius: 16px 16px 0 0; animation: rise .25s var(--ease) }
  .peek.sheet .th { border-radius: 16px 16px 0 0; height: 200px }
  .peek.sheet .acts button { padding: 10px 12px; font-size: 13.5px }
  @keyframes rise { from { transform: translateY(40px); opacity: 0 } }
</style>
