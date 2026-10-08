<script>
  // Tiny platform badge: WhatsApp or Telegram.
  let { jid = '', platform = '' } = $props()
  const p = $derived(platform || (jid.startsWith('tgbot:') ? 'telegram_bot' : jid.startsWith('tg:') ? 'telegram' : 'whatsapp'))
</script>

<span class="pf {p}" title={{ telegram: 'Telegram (your account)', telegram_bot: 'Telegram (bot)', whatsapp: 'WhatsApp' }[p]}>{{ telegram: 'TG', telegram_bot: 'TG bot', whatsapp: 'WA' }[p]}</span>

<style>
  .pf { font: 600 9px/1 var(--mono); letter-spacing: .04em; padding: 2px 4px; border-radius: 4px; flex: none }
  .whatsapp { background: #E3F5EA; color: #11663A }
  .telegram, .telegram_bot { background: #E2F1FB; color: #1B6A93 }
</style>
