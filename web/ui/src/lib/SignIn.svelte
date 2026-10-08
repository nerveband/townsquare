<script>
  import { onMount } from 'svelte'
  import { api } from './state.svelte.js'

  let st = $state(null) // { whatsapp, telegram }
  let state = $state('idle') // idle, sending, sent, error
  let sentTo = $state('')
  let msg = $state('')
  onMount(async () => { try { st = await api('GET', '/api/auth/status') } catch { st = { whatsapp: false, telegram: false } } })

  async function send(via) {
    state = 'sending'
    try { const r = await api('POST', '/api/auth/request', { via }); sentTo = r.sent_to; state = 'sent' } catch (e) { state = 'error'; msg = e.message }
  }
</script>

<div class="wrap">
  <div class="card">
    <div class="logo display"><img src="/logo-64.png" alt="" width="28" height="28" />Townsquare</div>
    <h1 class="display">Sign in</h1>
    {#if state === 'sent'}
      <p>We sent a one-time sign-in link to your own <b>{sentTo}</b> chat.</p>
      <p>Open it <b>on this device</b> within 15 minutes. It works once.</p>
      <button class="btn" onclick={() => location.reload()}>I've opened the link</button>
    {:else}
      <p>This browser isn't signed in yet. Townsquare sends a one-time link to <b>your own chat</b> on the phone you connected, so only you can sign in.</p>
      <div class="opts">
        <button class="btn pri big" onclick={() => send('whatsapp')} disabled={state === 'sending' || (st && !st.whatsapp)}>
          Send link to my WhatsApp<small>arrives in “Message yourself”</small>
        </button>
        {#if st?.telegram}
          <button class="btn big tg" onclick={() => send('telegram')} disabled={state === 'sending'}>
            Send link to my Telegram<small>arrives in “Saved Messages”</small>
          </button>
        {/if}
      </div>
      {#if st && !st.whatsapp && !st.telegram}<p class="err">Neither WhatsApp nor Telegram is connected right now, so a link can't be sent. Use the option below.</p>{/if}
      {#if state === 'error'}<p class="err">{msg}</p>{/if}
    {/if}
    <p class="muted small">Or, on the computer running Townsquare, run <code>townsquare login-link</code> and open the link it prints.</p>
  </div>
</div>

<style>
  .wrap { position: fixed; inset: 0; background: linear-gradient(180deg, #CFE6FB, #EEF3F8 60%); display: grid; place-items: center; z-index: 900; padding: 16px }
  .card { background: #fff; border-radius: 16px; padding: 28px; width: min(440px, 100%); box-shadow: var(--shadow); display: flex; flex-direction: column; gap: 12px }
  .logo { display: flex; align-items: center; gap: 8px; font-size: 22px; color: var(--t900) }
  .logo img { width: 28px; height: 28px; display: block }
  h1 { font-size: 44px; color: var(--t800); margin: 4px 0 0 }
  p { margin: 0; font-size: 14.5px; color: var(--ink2); line-height: 1.5 }
  .opts { display: flex; flex-direction: column; gap: 8px }
  .big { padding: 10px 14px; font-size: 14.5px; display: flex; flex-direction: column; align-items: center; gap: 1px }
  .big small { font-size: 11.5px; font-weight: 400; opacity: .8 }
  .tg { border-color: #9CC8F0; color: #164E6D }
  .err { color: #9B1C2C; background: #FDE4E7; padding: 8px 10px; border-radius: 8px; font-size: 13px }
  .small { font-size: 12.5px }
  code { font: 12px var(--mono); background: var(--sunk); padding: 1px 5px; border-radius: 4px }
</style>
