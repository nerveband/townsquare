<script>
  import { app, api, act, toast, loadState } from './state.svelte.js'
  import Platform from './Platform.svelte'

  let days = $state(+(localStorage.getItem('townsquare.statsDays') || 30))
  let platforms = $state(['whatsapp', 'telegram'])
  let chat = $state('') // one chat in focus, '' = all
  let data = $state(null)
  let err = $state('')
  let sharing = $state(false)
  let shareTo = $state('me')

  const query = $derived.by(() => {
    const q = new URLSearchParams({ days: String(days) })
    if (platforms.length < 2) q.set('platform', platforms.join(',') || 'none')
    const tagIds = [...app.tags.map((t) => t.id), 0].filter((id) => !app.hiddenTags.includes(id))
    if (app.hiddenTags.length) q.set('tag', tagIds.join(',') || '-1')
    const clientIds = [...app.clients.map((c) => c.id), 0].filter((id) => !app.hiddenClients.includes(id))
    if (app.hiddenClients.length) q.set('client', clientIds.join(',') || '-1')
    if (chat) q.set('chat', chat)
    return q.toString()
  })

  $effect(() => {
    const q = query
    localStorage.setItem('townsquare.statsDays', String(days))
    api('GET', `/api/stats/summary?${q}`).then((d) => { data = d; err = '' }).catch((e) => (err = e.message))
  })

  // ---- formatting ----
  const pct = (v) => `${Math.round((v || 0) * 100)}%`
  const num = (v) => (v >= 10000 ? `${(v / 1000).toFixed(v >= 100000 ? 0 : 1)}k` : Math.round(v || 0).toLocaleString())
  const dec = (v) => (v >= 10 ? Math.round(v) : (Math.round((v || 0) * 10) / 10).toString())
  function delta(cur, prev) {
    if (!data || !data.previous.sends || !prev) return null
    const d = (cur - prev) / prev
    if (Math.abs(d) < 0.01) return { t: 'same', c: '' }
    return { t: `${d > 0 ? '+' : ''}${Math.round(d * 100)}%`, c: d > 0 ? 'up' : 'down' }
  }
  const dayShort = (d) => new Date(d + 'T12:00').toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
  const DOW = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']

  // ---- chart geometry ----
  const W = 640, H = 170, HW = 340, HH = 190, PAD = { l: 34, r: 8, t: 10, b: 22 }
  function scaleY(max) { const m = Math.max(1, max); return (v) => PAD.t + (H - PAD.t - PAD.b) * (1 - v / m) }
  function niceMax(v) { if (v <= 5) return 5; const p = Math.pow(10, Math.floor(Math.log10(v))); return Math.ceil(v / p) * p }
  const reach = $derived.by(() => {
    if (!data) return null
    const pts = data.daily
    const max = niceMax(Math.max(...pts.map((d) => d.reach), 1))
    const y = scaleY(max)
    const x = (i) => PAD.l + (W - PAD.l - PAD.r) * (pts.length === 1 ? 0.5 : i / (pts.length - 1))
    const line = pts.map((d, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(d.reach).toFixed(1)}`).join('')
    const area = `${line}L${x(pts.length - 1)},${y(0)}L${x(0)},${y(0)}Z`
    const ticks = [0, max / 2, max]
    const every = Math.max(1, Math.ceil(pts.length / 8))
    return { pts, x, y, line, area, ticks, every }
  })
  const health = $derived.by(() => {
    if (!data) return null
    const pts = data.daily
    const max = niceMax(Math.max(...pts.map((d) => d.sent + d.held + d.failed + d.missed), 1))
    const y = (v) => PAD.t + (HH - PAD.t - PAD.b) * (1 - v / max)
    const bw = (HW - PAD.l - PAD.r) / pts.length
    const every = Math.max(1, Math.ceil(pts.length / 4))
    return { pts, y, bw, max, every }
  })
  const heatMax = $derived(data ? Math.max(0.01, ...data.heatmap.flat().map((c) => c.rate)) : 1)
  const best = $derived.by(() => {
    if (!data) return null
    let b = null
    data.heatmap.forEach((row, d) => row.forEach((c, h) => { if (c.sends && (!b || c.rate > b.rate)) b = { d, h, ...c } }))
    return b
  })
  const hourLabel = (h) => `${h % 12 || 12}${h < 12 ? 'a' : 'p'}`
  function spark(vals) {
    const m = Math.max(1, ...vals)
    return vals.map((v, i) => `${(i / Math.max(1, vals.length - 1)) * 80},${18 - (v / m) * 16}`).join(' ')
  }
  const emojiMax = $derived(data?.emoji?.length ? data.emoji[0].count : 1)
  const chatName = $derived(chat ? (app.targets.find((t) => t.jid === chat)?.name || chat) : '')
  const allowed = $derived(app.targets.filter((t) => t.allowed && !t.gone && t.can_send))

  function togglePlatform(p) { platforms = platforms.includes(p) ? platforms.filter((x) => x !== p) : [...platforms, p] }
  async function setNames(on) {
    await act(api('PUT', '/api/settings', { stats_people: on ? '1' : '0' }), on ? 'Who read it lists on' : 'Who read it lists off (names removed)')
    await loadState()
    data = await api('GET', `/api/stats/summary?${query}`)
  }
  async function copySummary() {
    const t = await (await fetch(`/api/stats/summary.txt?${query}`)).text()
    await navigator.clipboard.writeText(t)
    toast('Summary copied')
  }
  async function share() {
    try {
      const r = await api('POST', `/api/stats/share?${query}`, { to: shareTo })
      toast(`Summary sent to ${r.sent_to}`)
      sharing = false
    } catch (e) { toast(e.message) }
  }
</script>

<div class="stats">
  <div class="top">
    <div class="seg">{#each [7, 30, 90] as d}<button class:on={days === d} onclick={() => (days = d)}>{d} days</button>{/each}</div>
    <label class="pf"><input type="checkbox" checked={platforms.includes('whatsapp')} onchange={() => togglePlatform('whatsapp')} />WhatsApp</label>
    <label class="pf"><input type="checkbox" checked={platforms.includes('telegram')} onchange={() => togglePlatform('telegram')} />Telegram</label>
    {#if chat}<span class="focus">{chatName} <button onclick={() => (chat = '')} aria-label="Show all chats">×</button></span>{/if}
    <span class="sp"></span>
    <button class="btn sm" onclick={copySummary}>Copy summary</button>
    <button class="btn sm" onclick={() => (sharing = !sharing)}>Send summary…</button>
    <a class="btn sm" href={`/api/stats/export.csv?${query}`} download>Download CSV</a>
  </div>
  {#if sharing}
    <div class="share">
      <span>Send this summary now to:</span>
      <label><input type="radio" bind:group={shareTo} value="me" /> Message yourself</label>
      {#each allowed.filter((t) => t.kind !== 'self' && t.kind !== 'status').slice(0, 8) as t (t.jid)}
        <label><input type="radio" bind:group={shareTo} value={t.jid} /> {t.name}</label>
      {/each}
      <button class="btn sm pri" onclick={share}>Send</button>
      <button class="btn sm" onclick={() => (sharing = false)}>Cancel</button>
      {#if app.settings.safe_mode === '1'}<small class="muted">Safe mode is on, so only allowlisted chats are listed.</small>{/if}
    </div>
  {/if}

  {#if err}<p class="err">{err}</p>{/if}
  {#if !data}<p class="muted">Loading…</p>
  {:else}
    <p class="note">
      {#if data.tracking_since}Counting since {new Date(data.tracking_since * 1000).toLocaleDateString(undefined, { month: 'long', day: 'numeric', year: 'numeric' })}.{:else}Nothing counted yet: numbers start with the next post Townsquare sends.{/if}
      Reads come from members' phones, so a few may be missing. Channels and Telegram report views instead of reads.
      Tag and client checkboxes on the left filter this page too.
    </p>

    <div class="tiles">
      {#each [
        ['Posts', num(data.tiles.posts), `${data.tiles.sends} sends · ${pct(data.tiles.on_time)} on time`, delta(data.tiles.posts, data.previous.posts)],
        ['Reach', num(data.tiles.reach), 'reads and views', delta(data.tiles.reach, data.previous.reach)],
        ['Read rate', pct(data.tiles.read_rate), 'of members, on average', delta(data.tiles.read_rate, data.previous.read_rate)],
        ['Reactions', dec(data.tiles.reactions_per_post), `per post · ${num(data.tiles.reactions)} total`, delta(data.tiles.reactions_per_post, data.previous.reactions_per_post)],
        ['Replies', dec(data.tiles.replies_per_post), `per post · ${num(data.tiles.replies)} total`, delta(data.tiles.replies_per_post, data.previous.replies_per_post)],
        ['Members', `${data.member_growth > 0 ? '+' : ''}${num(data.member_growth)}`, 'joined minus left', null],
      ] as [l, v, sub, d]}
        <div class="tile">
          <span class="tl">{l}</span>
          <b class="tv">{v}</b>
          <span class="ts">{sub}{#if d}<i class={d.c}>{d.t}</i>{/if}</span>
        </div>
      {/each}
    </div>

    <div class="grid">
      <section class="card wide">
        <h3>Reach over time <small>people who read or viewed, by day posted</small></h3>
        <svg viewBox="0 0 {W} {H}" class="chart" role="img" aria-label="Reach by day">
          <defs><linearGradient id="rg" x1="0" x2="0" y1="0" y2="1"><stop offset="0" stop-color="#1673E6" stop-opacity=".28" /><stop offset="1" stop-color="#1673E6" stop-opacity="0" /></linearGradient></defs>
          {#each reach.ticks as t}<line x1={PAD.l} x2={W - PAD.r} y1={reach.y(t)} y2={reach.y(t)} class="gl" /><text x={PAD.l - 5} y={reach.y(t) + 3} class="ax" text-anchor="end">{num(t)}</text>{/each}
          <path d={reach.area} fill="url(#rg)" />
          <path d={reach.line} fill="none" stroke="#1673E6" stroke-width="2" stroke-linejoin="round" />
          {#each reach.pts as p, i}
            {#if p.posts}<circle cx={reach.x(i)} cy={reach.y(p.reach)} r="3.2" fill="#FFD21F" stroke="#0E2A47" stroke-width="1"><title>{dayShort(p.day)}: {p.reach} reached, {p.posts} posts</title></circle>{/if}
            {#if i % reach.every === 0}<text x={reach.x(i)} y={H - 6} class="ax" text-anchor="middle">{dayShort(p.day)}</text>{/if}
          {/each}
        </svg>
        <div class="legend"><span><i style="background:#1673E6"></i>Reach</span><span><i class="dot"></i>Days with posts</span></div>
      </section>

      <section class="card">
        <h3>Read speed <small>share of members reached after</small></h3>
        <div class="speed">
          {#each [['1h', '1 hour'], ['6h', '6 hours'], ['24h', '1 day'], ['7d', '1 week']] as [k, l]}
            <div class="sr"><span>{l}</span><div class="bar"><div style="width:{pct(data.speed[k].rate)}"></div></div><b>{pct(data.speed[k].rate)}</b></div>
          {/each}
        </div>
        <p class="hint">{#if data.speed['1h'].sends}Within an hour, {pct(data.speed['1h'].rate)} of members see a post. After a day, {pct(data.speed['24h'].rate)}.{:else}Shows up an hour after the first post.{/if}</p>
      </section>

      <section class="card">
        <h3>Best time to post <small>1-hour read rate by when it was sent</small></h3>
        <div class="heat">
          <span></span>{#each Array(24) as _, h}<span class="hh">{h % 6 === 0 ? hourLabel(h) : ''}</span>{/each}
          {#each data.heatmap as row, d}
            <span class="hd">{DOW[d]}</span>
            {#each row as c, h}<span class="hc" style="--a:{c.sends ? 0.12 + 0.88 * (c.rate / heatMax) : 0}" class:empty={!c.sends} title={c.sends ? `${DOW[d]} ${hourLabel(h)}: ${pct(c.rate)} in the first hour (${c.sends} sends)` : `${DOW[d]} ${hourLabel(h)}: no posts yet`}></span>{/each}
          {/each}
        </div>
        <p class="hint">{#if best}Best so far: <b>{DOW[best.d]} around {hourLabel(best.h)}</b>, {pct(best.rate)} read in the first hour.{:else}Fills in as you post at different times.{/if}</p>
      </section>

      <section class="card">
        <h3>Delivery health <small>per day</small></h3>
        <svg viewBox="0 0 {HW} {HH}" class="chart" role="img" aria-label="Deliveries by day">
          {#each [0, health.max / 2, health.max] as t}<line x1={PAD.l} x2={HW - PAD.r} y1={health.y(t)} y2={health.y(t)} class="gl" /><text x={PAD.l - 5} y={health.y(t) + 3} class="ax" text-anchor="end">{Math.round(t)}</text>{/each}
          {#each health.pts as p, i}
            {@const x = PAD.l + i * health.bw + health.bw * 0.15}
            {@const w = health.bw * 0.7}
            {@const segs = [['sent', '#2DB34E'], ['held', '#E5A50A'], ['failed', '#E0475B'], ['missed', '#9AA5A1']]}
            {#each segs as [k, c], si}
              {@const below = segs.slice(0, si).reduce((a, [kk]) => a + p[kk], 0)}
              {#if p[k]}<rect x={x} width={w} y={health.y(below + p[k])} height={health.y(below) - health.y(below + p[k])} fill={c} rx="1.5"><title>{dayShort(p.day)}: {p[k]} {k}</title></rect>{/if}
            {/each}
            {#if i % health.every === 0}<text x={x + w / 2} y={HH - 6} class="ax" text-anchor="middle">{dayShort(p.day)}</text>{/if}
          {/each}
        </svg>
        <div class="legend"><span><i style="background:#2DB34E"></i>Sent</span><span><i style="background:#E5A50A"></i>Held</span><span><i style="background:#E0475B"></i>Failed</span><span><i style="background:#9AA5A1"></i>Missed</span></div>
      </section>

      <section class="card">
        <h3>Reactions <small>most used</small></h3>
        {#if data.emoji.length}
          <div class="emoji">{#each data.emoji as e}<div><span class="em">{e.emoji}</span><div class="bar"><div style="width:{(e.count / emojiMax) * 100}%"></div></div><b>{num(e.count)}</b></div>{/each}</div>
        {:else}<p class="hint">No reactions yet.</p>{/if}
      </section>

      <section class="card wide">
        <h3>What works <small>average per send</small></h3>
        <div class="works">
          {#each [['By type', data.by_kind], ['By tag', data.by_tag], ['By client', data.by_client], ['By app', data.by_platform]] as [l, gs]}
            <div>
              <h4>{l}</h4>
              {#each gs as g (g.key)}
                <div class="wr" title="{g.sends} sends · {dec(g.reactions)} reactions and {dec(g.replies)} replies per send">
                  <span class="wl">{#if g.color}<i style="background:{g.color}"></i>{/if}{g.label}</span>
                  <div class="bar"><div style="width:{g.rate_sends ? pct(g.read_rate) : 0}"></div></div>
                  <b>{g.rate_sends ? pct(g.read_rate) : '–'}</b>
                </div>
              {:else}<p class="hint">Nothing yet.</p>{/each}
            </div>
          {/each}
        </div>
      </section>

      <section class="card wide">
        <h3>Chats <small>click one to focus the whole page on it</small></h3>
        <div class="tw">
          <table>
            <thead><tr><th>Chat</th><th class="n">Members</th><th class="n">Sends</th><th class="n">Reach</th><th>Read rate</th><th class="n">Reactions</th><th class="n">Replies</th><th class="n">Growth</th><th>Trend</th></tr></thead>
            <tbody>
              {#each data.chats as c (c.jid)}
                <tr class:sel={chat === c.jid} onclick={() => (chat = chat === c.jid ? '' : c.jid)}>
                  <td><div class="cn"><Platform jid={c.jid} /> {c.name}</div></td>
                  <td class="n">{c.members ? num(c.members) : '–'}</td><td class="n">{c.sends}</td><td class="n">{num(c.reach)}</td>
                  <td><div class="rr"><div class="bar"><div style="width:{c.members ? pct(c.read_rate) : 0}"></div></div><span>{c.members ? pct(c.read_rate) : '–'}</span></div></td>
                  <td class="n">{num(c.reactions)}</td><td class="n">{num(c.replies)}</td>
                  <td class="n" class:up={c.growth > 0} class:down={c.growth < 0}>{c.growth > 0 ? '+' : ''}{c.growth}</td>
                  <td><svg viewBox="0 0 80 20" class="spark"><polyline points={spark(c.trend)} fill="none" stroke="#1673E6" stroke-width="1.5" /></svg></td>
                </tr>
              {:else}<tr><td colspan="9" class="hint">No sends in this period.</td></tr>{/each}
            </tbody>
          </table>
        </div>
      </section>

      {#if Object.keys(data.insights).length}
        <section class="card wide">
          <h3>Telegram insights <small>Telegram's own numbers for larger channels</small></h3>
          <div class="ins">
            {#each Object.entries(data.insights) as [jid, x] (jid)}
              {@const d = x.data}
              <div class="insc">
                <b>{x.name}</b>
                {#each [['Followers', d.followers], ['Views per post', d.views_per_post], ['Shares per post', d.shares_per_post], ['Reactions per post', d.reactions_per_post]] as [l, v]}
                  {#if v}<div class="iv"><span>{l}</span><b>{num(v.current)}</b>{#if v.previous}<i class:up={v.current > v.previous} class:down={v.current < v.previous}>{v.current >= v.previous ? '+' : ''}{num(v.current - v.previous)}</i>{/if}</div>{/if}
                {/each}
                {#if d.notifications_on}<div class="iv"><span>Notifications on</span><b>{pct(d.notifications_on)}</b></div>{/if}
              </div>
            {/each}
          </div>
        </section>
      {/if}

      <section class="card wide privacy">
        <h3>Who read it lists</h3>
        <p class="hint">Off: Townsquare keeps only counts. On: it also keeps names for reads, reactions and replies so you can see them on each post. Names are deleted after 30 days, and right away if you turn this off. Everything stays on this computer.</p>
        <div class="seg"><button class:on={!data.names} onclick={() => setNames(false)}>Counts only</button><button class:on={data.names} onclick={() => setNames(true)}>Keep names</button></div>
      </section>
    </div>
  {/if}
</div>

<style>
  .stats { padding: 14px 18px 60px; display: flex; flex-direction: column; gap: 12px; max-width: 1400px }
  .top { display: flex; align-items: center; gap: 10px; flex-wrap: wrap }
  .sp { flex: 1 }
  .seg { display: inline-flex; background: var(--sunk); border-radius: 9px; padding: 2px }
  .seg button { border: 0; background: transparent; padding: 5px 10px; border-radius: 7px; font-size: 12.5px; color: var(--ink2) }
  .seg button.on { background: #fff; color: var(--ink); box-shadow: 0 1px 2px rgba(0,0,0,.08) }
  .pf { display: flex; align-items: center; gap: 5px; font-size: 12.5px; color: var(--ink2) }
  .pf input { accent-color: var(--sky) }
  .focus { background: var(--sky-soft); color: var(--t800); padding: 3px 4px 3px 10px; border-radius: 99px; font-size: 12.5px; font-weight: 500 }
  .focus button { border: 0; background: transparent; color: var(--t800); font-size: 15px; padding: 0 5px; line-height: 1 }
  a.btn { text-decoration: none; color: inherit }
  .share { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; background: var(--surface); border: 1px solid var(--line); border-radius: 10px; padding: 8px 12px; font-size: 12.5px }
  .share label { display: flex; gap: 4px; align-items: center }
  .note { margin: 0; font-size: 12px; color: var(--muted) }
  .err { color: #9B1C2C }
  .tiles { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); gap: 8px }
  .tile { background: var(--surface); border: 1px solid var(--line); border-radius: 12px; padding: 10px 12px; display: flex; flex-direction: column; gap: 2px; min-width: 0 }
  .tl { font-size: 11px; letter-spacing: .06em; text-transform: uppercase; color: var(--muted); font-weight: 600 }
  .tv { font: 600 30px/1.05 var(--display); color: var(--t900) }
  .ts { font-size: 11.5px; color: var(--muted); display: flex; gap: 6px; flex-wrap: wrap }
  .ts i, .ins i { font-style: normal; font-weight: 600 }
  .up { color: #1E8A3C } .down { color: #C2364B }
  .grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px }
  .card { background: var(--surface); border: 1px solid var(--line); border-radius: 12px; padding: 12px 14px; min-width: 0 }
  .card.wide { grid-column: span 3 }
  .grid > .card:nth-child(1) { grid-column: span 2 }
  h3 { margin: 0 0 8px; font: 600 18px var(--display); text-transform: uppercase; letter-spacing: .02em; color: var(--t900); display: flex; gap: 8px; align-items: baseline; flex-wrap: wrap }
  h3 small { font: 400 11.5px var(--sans); text-transform: none; color: var(--muted); letter-spacing: 0 }
  h4 { margin: 0 0 6px; font-size: 11px; text-transform: uppercase; letter-spacing: .06em; color: var(--muted) }
  .chart { width: 100%; height: auto; display: block }
  .gl { stroke: var(--line2); stroke-width: 1 }
  .ax { font: 10px var(--mono); fill: var(--muted) }
  .legend { display: flex; gap: 12px; font-size: 11.5px; color: var(--muted); margin-top: 4px }
  .legend i { display: inline-block; width: 10px; height: 10px; border-radius: 3px; margin-right: 4px; vertical-align: -1px }
  .legend i.dot { border-radius: 50%; background: #FFD21F; border: 1px solid #0E2A47 }
  .hint { font-size: 12px; color: var(--muted); margin: 8px 0 0 }
  .bar { flex: 1; height: 8px; background: var(--sunk); border-radius: 99px; overflow: hidden; min-width: 40px }
  .bar div { height: 100%; background: linear-gradient(90deg, #3BA0F2, #1673E6); border-radius: 99px }
  .speed { display: flex; flex-direction: column; gap: 9px; margin-top: 6px }
  .sr { display: grid; grid-template-columns: 62px 1fr 42px; align-items: center; gap: 8px; font-size: 12.5px }
  .sr b, .emoji b, .wr b { font: 500 12px var(--mono); text-align: right }
  .heat { display: grid; grid-template-columns: 28px repeat(24, minmax(0, 1fr)); gap: 2px; font-size: 9.5px; color: var(--muted) }
  .hh { font: 9px var(--mono); height: 12px; overflow: visible; white-space: nowrap }
  .hd { font-size: 10.5px; align-self: center }
  .hc { aspect-ratio: 1; border-radius: 3px; background: color-mix(in srgb, #1673E6 calc(var(--a) * 100%), #EEF3F8) }
  .hc.empty { background: #F3F5F7 }
  .emoji { display: flex; flex-direction: column; gap: 6px }
  .emoji > div { display: grid; grid-template-columns: 24px 1fr 46px; align-items: center; gap: 8px }
  .em { font-size: 16px }
  .works { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 16px }
  .wr { display: grid; grid-template-columns: minmax(0, 1fr) 1fr 40px; gap: 8px; align-items: center; font-size: 12.5px; margin-bottom: 5px }
  .wl { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; display: flex; align-items: center; gap: 5px }
  .wl i { width: 8px; height: 8px; border-radius: 2px; flex: none }
  .tw { overflow-x: auto }
  table { width: 100%; border-collapse: collapse; font-size: 12.5px }
  th { text-align: left; font-size: 10.5px; text-transform: uppercase; letter-spacing: .05em; color: var(--muted); font-weight: 600; padding: 4px 8px; border-bottom: 1px solid var(--line) }
  td { padding: 6px 8px; border-bottom: 1px solid var(--line2); white-space: nowrap }
  td.n, th.n { text-align: right; font-family: var(--mono); font-size: 12px }
  tbody tr { cursor: pointer }
  tbody tr:hover { background: var(--sunk) }
  tr.sel { background: var(--sky-soft) }
  .cn { display: flex; align-items: center; gap: 6px; max-width: 280px; overflow: hidden; text-overflow: ellipsis }
  .rr { display: flex; align-items: center; gap: 6px; min-width: 120px }
  .rr span { font: 12px var(--mono); width: 36px; text-align: right }
  .spark { width: 80px; height: 20px; display: block }
  .ins { display: grid; grid-template-columns: repeat(auto-fill, minmax(230px, 1fr)); gap: 10px }
  .insc { border: 1px solid var(--line2); border-radius: 10px; padding: 10px; display: flex; flex-direction: column; gap: 4px; font-size: 12.5px }
  .iv { display: flex; gap: 6px; align-items: baseline }
  .iv span { flex: 1; color: var(--ink2) }
  .iv b { font: 500 13px var(--mono) }
  .privacy .seg { margin-top: 8px }
  @media (max-width: 1100px) {
    .tiles { grid-template-columns: repeat(3, minmax(0, 1fr)) }
    .grid { grid-template-columns: repeat(2, minmax(0, 1fr)) }
    .card.wide, .grid > .card:nth-child(1) { grid-column: span 2 }
    .works { grid-template-columns: repeat(2, minmax(0, 1fr)) }
  }
  @media (max-width: 700px) {
    .stats { padding: 10px 10px 80px }
    .tiles { grid-template-columns: repeat(2, minmax(0, 1fr)) }
    .grid { grid-template-columns: 1fr }
    .card.wide, .grid > .card:nth-child(1) { grid-column: span 1 }
    .works { grid-template-columns: 1fr }
    .tv { font-size: 26px }
  }
</style>
