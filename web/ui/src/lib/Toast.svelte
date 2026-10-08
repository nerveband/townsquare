<script>
  import { app } from './state.svelte.js'
</script>

{#if app.toast}
  <div class="toast-wrap">
    <div class="toast" role="status" aria-live="polite">
      <span>{app.toast.msg}</span>
      {#each app.toast.actions as a}
        <button class:u={a.primary} onclick={() => { app.toast = null; a.run() }}>{a.label}</button>
      {/each}
    </div>
  </div>
{/if}

<style>
  .toast { background: var(--ink); color: #fff; border-radius: 10px; padding: 8px 8px 8px 14px; display: flex; gap: 10px; align-items: center; font-size: 12.5px; box-shadow: 0 12px 32px rgba(0,0,0,.25); animation: up .22s var(--ease); max-width: 92vw }
  @keyframes up { from { transform: translateY(12px); opacity: 0 } }
  button { border: 0; border-radius: 6px; padding: 4px 9px; font-size: 12px; font-weight: 600; background: rgba(255,255,255,.12); color: #fff; white-space: nowrap }
  button.u { background: var(--sun); color: var(--t900) }
</style>
