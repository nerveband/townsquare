<script>
  // Slide through time zones and watch every time on screen change live.
  // This is a preview only; "Make default" saves it in Settings.
  import { onMount } from 'svelte'
  import { app, api, act, tz, defaultTZ, loadSends } from './state.svelte.js'
  import { ZONE_NAMES, zoneName, tzShort, time12, offsetMin, offsetLabel } from './time.js'

  let open = $state(false)
  let root = $state()
  let now = $state(Date.now())
  onMount(() => {
    const t = setInterval(() => (now = Date.now()), 30000)
    return () => clearInterval(t)
  })

  const zones = $derived.by(() => {
    const set = new Set([...Object.keys(ZONE_NAMES), defaultTZ(), tz()])
    set.delete('UTC')
    return [...set].sort((a, b) => offsetMin(a) - offsetMin(b))
  })
  const idx = $derived(Math.max(0, zones.indexOf(tz())))
  const previewing = $derived(!!app.viewTZ && app.viewTZ !== defaultTZ())
  const diff = $derived(offsetMin(tz()) - offsetMin(defaultTZ()))

  function setZone(z) {
    app.viewTZ = z === defaultTZ() ? '' : z
    loadSends()
  }
  async function makeDefault() {
    const z = tz()
    await act(api('PUT', '/api/settings', { timezone: z }), `${zoneName(z)} is now your default time zone`)
    app.viewTZ = ''
  }
</script>

<svelte:window onclick={(e) => { if (open && root && !root.contains(e.target)) open = false }} onkeydown={(e) => { if (open && e.key === 'Escape') open = false }} />

<div class="tzs" bind:this={root}>
  <button class="trigger" class:pv={previewing} onclick={() => (open = !open)} aria-expanded={open} title="Preview times in another zone">
    <span class="dot" class:on={app.connected} class:demo={app.demo} title={app.demo ? 'Demo mode' : app.connected ? 'WhatsApp connected' : 'WhatsApp not connected'}></span>
    <b>{time12(now, tz())}</b><span>{tzShort(tz())}</span>
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4"><path d="M6 9l6 6 6-6" /></svg>
  </button>
  {#if open}
    <div class="pop">
      <div class="big">
        <b class="display">{time12(now, tz())}</b>
        <span>{zoneName(tz())} · {tzShort(tz())}</span>
      </div>
      <input type="range" min="0" max={zones.length - 1} step="1" value={idx} oninput={(e) => setZone(zones[+e.target.value])} aria-label="Time zone" />
      <div class="ticks">
        {#each zones as z, i}<button class:on={i === idx} class:def={z === defaultTZ()} style="left:{(i / (zones.length - 1)) * 100}%" onclick={() => setZone(z)} title={zoneName(z)}></button>{/each}
      </div>
      <p class="cmp">
        {#if previewing}
          <b>{offsetLabel(diff)}</b> vs your default ({zoneName(defaultTZ())}, now {time12(now, defaultTZ())}). Every time on screen is shown in {zoneName(tz())}.
        {:else}
          This is your default zone. Slide to preview how every post's time reads somewhere else.
        {/if}
      </p>
      <div class="quick">
        {#each ['America/Los_Angeles', 'America/Denver', 'America/Chicago', 'America/New_York', 'Europe/London', 'Asia/Karachi'] as z}
          <button class:on={z === tz()} onclick={() => setZone(z)}>{zoneName(z)}</button>
        {/each}
      </div>
      {#if previewing}
        <div class="acts">
          <button class="btn sm" onclick={() => setZone(defaultTZ())}>Back to {zoneName(defaultTZ())}</button>
          <button class="btn sm pri" onclick={makeDefault}>Make {zoneName(tz())} my default</button>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .tzs { position: relative }
  .trigger { display: inline-flex; align-items: center; gap: 6px; border: 1px solid var(--line); background: #fff; border-radius: 8px; padding: 5px 8px; font-size: 12px; color: var(--ink2); white-space: nowrap }
  .trigger b { font-weight: 600; color: var(--ink); font-variant-numeric: tabular-nums }
  .trigger span { font: 500 11px var(--mono); color: var(--muted) }
  .trigger svg { width: 12px; height: 12px; opacity: .6 }
  .trigger.pv { border-color: #9CC8F0; background: #EEF6FD }
  .trigger.pv span { color: #1B6A93 }
  .dot { width: 7px; height: 7px; border-radius: 50%; background: var(--rose) }
  .dot.on { background: var(--grass) }
  .dot.demo { background: #53BDEB }
  .pop { position: absolute; right: 0; top: calc(100% + 6px); width: 340px; background: #fff; border: 1px solid var(--line); border-radius: 12px; box-shadow: var(--shadow); padding: 14px; z-index: 350; display: flex; flex-direction: column; gap: 10px }
  .big { display: flex; align-items: baseline; gap: 10px }
  .big b { font-size: 38px; color: var(--t800) }
  .big span { color: var(--ink2); font-size: 13px }
  input[type=range] { width: 100%; accent-color: var(--t800); margin: 0 }
  .ticks { position: relative; height: 10px; margin: -6px 8px 0 }
  .ticks button { position: absolute; transform: translateX(-50%); width: 6px; height: 6px; padding: 0; border: 0; border-radius: 50%; background: #CFD6D3 }
  .ticks button.def { background: var(--t600); width: 8px; height: 8px }
  .ticks button.on { background: var(--sky); box-shadow: 0 0 0 2px #fff, 0 0 0 3px var(--sky) }
  .cmp { margin: 0; font-size: 12.5px; color: var(--ink2); line-height: 1.45 }
  .cmp b { color: #1B6A93 }
  .quick { display: flex; flex-wrap: wrap; gap: 4px }
  .quick button { border: 1px solid var(--line); background: #fff; border-radius: 99px; padding: 3px 9px; font-size: 12px }
  .quick button.on { background: var(--t800); border-color: var(--t800); color: #fff }
  .acts { display: flex; gap: 6px; justify-content: flex-end; border-top: 1px solid var(--line2); padding-top: 10px }
  @media (max-width: 700px) {
    .trigger b, .trigger svg { display: none }
    .pop { position: fixed; left: 8px; right: 8px; top: auto; bottom: 8px; width: auto }
  }
</style>
