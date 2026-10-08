<script>
  import { waHTML } from './wafmt.js'
  import { app, checkAuth } from './state.svelte.js'
  let { media = [], caption = '', big = false } = $props()
  const first = $derived(media.length ? app.media[media[0]] || { kind: 'image', id: media[0] } : null)
  const bars = [10, 22, 14, 30, 12, 24, 8, 18, 26, 10, 16, 22, 12]
  let failed = $state(false)
  const dur = (s) => (s ? `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}` : '')
</script>

<div class="thumb" class:big>
  {#if !first}
    <div class="th-text"><p class="wa">{#if caption}{@html waHTML(caption)}{:else}Empty message{/if}</p></div>
  {:else if first.kind === 'image' || first.kind === 'video'}
    {#if failed}<div class="th-doc">{first.kind === 'video' ? 'Video' : 'Photo'}</div>
    {:else}<img class="th-img" src="/api/media/{first.id}/preview" alt="" loading="lazy" onerror={() => { failed = true; checkAuth() }} />{/if}
    {#if first.kind === 'video'}<span class="play"></span><span class="badge" style="right:5px;bottom:5px">{dur(first.seconds)}</span>{/if}
  {:else if first.kind === 'voice' || first.kind === 'audio'}
    <div class="th-voice">{#each bars as h}<i style="height:{big ? h * 1.3 : h * 0.8}px"></i>{/each}</div>
    <span class="badge" style="right:5px;bottom:5px">{first.kind === 'voice' ? 'VOICE' : 'AUDIO'} {dur(first.seconds)}</span>
  {:else}
    <div class="th-doc">{(first.name || 'file').split('.').pop()}</div>
  {/if}
  {#if media.length > 1}<span class="badge" style="left:5px;top:5px">+{media.length - 1}</span>{/if}
</div>

<style>
  .thumb { position: relative; width: 100%; height: 100%; overflow: hidden }
  .play { position: absolute; left: 50%; top: 50%; width: 28px; height: 28px; margin: -14px 0 0 -14px; border-radius: 50%; background: rgba(17,27,33,.55) }
  .play::after { content: ""; position: absolute; left: 11px; top: 8px; border-left: 10px solid #fff; border-top: 6px solid transparent; border-bottom: 6px solid transparent }
  .big .th-text p { font-size: 12.5px }
</style>
