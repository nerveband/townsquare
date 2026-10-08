<script>
  // Shows a post the way it will arrive. WhatsApp: each media item is its own
  // message, the caption rides on the first one (a voice note's caption follows
  // as text). Telegram: photos and videos arrive as one album with the caption
  // under it; other files follow one by one.
  import { app, checkAuth } from './state.svelte.js'
  import { waHTML } from './wafmt.js'

  let { caption = '', media = [], time = '', compact = false, platform = 'whatsapp' } = $props()
  const info = (id) => app.media[id] || { id, kind: 'image', name: '' }
  const dur = (s) => (s ? `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}` : '0:00')
  const bars = [6, 12, 18, 10, 22, 14, 8, 20, 16, 26, 12, 18, 9, 15, 22, 11, 7, 17, 24, 13, 10, 19, 8, 14]

  // Build the list of messages that will actually be sent.
  const msgs = $derived.by(() => {
    if (!media.length) return caption.trim() ? [{ kind: 'text', text: caption }] : []
    if (platform === 'telegram') {
      const vis = media.map(info).filter((m) => m.kind === 'image' || m.kind === 'video')
      const rest = media.map(info).filter((m) => m.kind !== 'image' && m.kind !== 'video')
      const out = []
      if (vis.length) out.push({ kind: 'album', items: vis, text: caption })
      rest.forEach((m, i) => out.push({ kind: m.kind, m, text: !vis.length && i === 0 ? caption : '' }))
      return out
    }
    const out = []
    media.forEach((id, i) => {
      const m = info(id)
      const cap = i === 0 && m.kind !== 'voice' ? caption : ''
      out.push({ kind: m.kind, m, text: cap })
      if (i === 0 && m.kind === 'voice' && caption.trim()) out.push({ kind: 'text', text: caption })
    })
    return out
  })
  let broken = $state({})
</script>

<div class="wap" class:compact class:tg={platform === 'telegram'}>
  {#each msgs as msg, i}
    <div class="bub" class:media={msg.kind === 'image' || msg.kind === 'video' || msg.kind === 'album'}>
      {#if msg.kind === 'album'}
        <div class="album" class:one={msg.items.length === 1}>
          {#each msg.items.slice(0, 10) as m (m.id)}
            <div class="vis">
              {#if broken[m.id]}<div class="ph">{m.kind === 'video' ? 'Video' : 'Photo'}</div>
              {:else}<img src="/api/media/{m.id}/preview" alt={m.name} onerror={() => { broken = { ...broken, [m.id]: true }; checkAuth() }} />{/if}
              {#if m.kind === 'video'}<span class="play"></span><span class="dur">▶ {dur(m.seconds)}</span>{/if}
            </div>
          {/each}
        </div>
      {:else if msg.kind === 'image' || msg.kind === 'video'}
        <div class="vis">
          {#if broken[msg.m.id]}<div class="ph">{msg.m.kind === 'video' ? 'Video' : 'Photo'}</div>
          {:else}<img src="/api/media/{msg.m.id}/preview" alt={msg.m.name} onerror={() => { broken = { ...broken, [msg.m.id]: true }; checkAuth() }} />{/if}
          {#if msg.kind === 'video'}<span class="play"></span><span class="dur">▶ {dur(msg.m.seconds)}</span>{/if}
        </div>
      {:else if msg.kind === 'voice' || msg.kind === 'audio'}
        <div class="voice">
          <span class="pl">▶</span>
          <span class="wave">{#each bars as h}<i style="height:{h}px"></i>{/each}</span>
          <span class="vd">{dur(msg.m.seconds)}</span>
        </div>
      {:else if msg.kind === 'document'}
        <div class="doc"><span class="fi">{(msg.m.name || 'file').split('.').pop().toUpperCase().slice(0, 4)}</span><span class="fn">{msg.m.name || 'Document'}</span></div>
      {/if}
      {#if msg.text}<div class="wa txt">{@html waHTML(msg.text)}</div>{/if}
      <span class="meta">{time}<b>✓✓</b></span>
    </div>
  {:else}
    <p class="empty">Your message will show here, formatted the way {platform === 'telegram' ? 'Telegram' : 'WhatsApp'} shows it.</p>
  {/each}
</div>

<style>
  .wap { display: flex; flex-direction: column; align-items: flex-end; gap: 6px }
  .bub { position: relative; max-width: min(100%, 420px); min-width: 120px; background: var(--bubble); border-radius: 9px 2px 9px 9px; padding: 5px 7px 18px; box-shadow: 0 1px .5px rgba(0,0,0,.13); font-size: 14px; color: var(--ink) }
  .compact .bub { font-size: 13px }
  .bub.media { padding: 3px 3px 18px }
  .vis { position: relative; border-radius: 6px; overflow: hidden; background: #cfd8d4 }
  .vis img { display: block; width: 100%; max-height: 360px; object-fit: cover }
  .compact .vis img { max-height: 200px }
  .ph { height: 160px; display: grid; place-items: center; color: #5D6E68; font-size: 12px }
  .play { position: absolute; left: 50%; top: 50%; width: 44px; height: 44px; margin: -22px 0 0 -22px; border-radius: 50%; background: rgba(17,27,33,.55) }
  .play::after { content: ""; position: absolute; left: 17px; top: 13px; border-left: 14px solid #fff; border-top: 9px solid transparent; border-bottom: 9px solid transparent }
  .dur { position: absolute; left: 6px; bottom: 5px; color: #fff; font-size: 11px; text-shadow: 0 1px 2px rgba(0,0,0,.5) }
  .txt { padding: 4px 4px 0 }
  .bub.media .txt { padding: 6px 6px 0 }
  .meta { position: absolute; right: 8px; bottom: 3px; font-size: 10.5px; color: #667781; display: flex; gap: 3px }
  .meta b { color: #53BDEB; font-weight: 400 }
  .voice { display: flex; align-items: center; gap: 8px; padding: 6px 4px; width: 260px; max-width: 100% }
  .pl { width: 30px; height: 30px; border-radius: 50%; background: var(--t600); color: #fff; display: grid; place-items: center; font-size: 11px; flex: none }
  .wave { flex: 1; display: flex; align-items: center; gap: 2px; height: 28px }
  .wave i { width: 3px; border-radius: 2px; background: #8FA19A }
  .vd { font-size: 11px; color: #667781 }
  .doc { display: flex; align-items: center; gap: 8px; background: rgba(0,0,0,.04); border-radius: 6px; padding: 8px; width: 260px; max-width: 100% }
  .fi { background: #E0475B; color: #fff; font: 600 10px var(--mono); border-radius: 4px; padding: 6px 4px; flex: none }
  .fn { font-size: 13px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis }
  .empty { margin: 0; align-self: center; color: #5D6E68; font-size: 12.5px; padding: 18px 0 }
  .album { display: grid; grid-template-columns: 1fr 1fr; gap: 2px; border-radius: 10px; overflow: hidden }
  .album.one { grid-template-columns: 1fr }
  .album .vis { border-radius: 0 }
  .album .vis img { height: 100%; min-height: 110px; max-height: 260px }
  /* Telegram: light green outgoing bubbles, rounder corners, green ticks */
  .tg .bub { background: #EFFDDE; border-radius: 14px 14px 4px 14px; box-shadow: 0 1px 1px rgba(16, 35, 47, .15) }
  .tg .meta { color: #5FA752 }
  .tg .meta b { color: #4FAE4E }
  .tg .pl { background: #4FAE4E }
</style>
