<script>
  import { app, loadHistory, undo, redo } from './state.svelte.js'
  import { ago } from './time.js'
  import Tip from './Tip.svelte'
  $effect(() => { if (app.showHistory) loadHistory() })
  const who = (a) => (a === 'sender' ? { t: '⚙', c: 'sys', n: 'Sender' } : a === 'you' ? { t: 'Y', c: '', n: 'You' } : { t: '{}', c: 'api', n: a.startsWith('api:') ? `API key “${a.slice(4)}”` : a })
</script>

<aside class="hist" class:open={app.showHistory} aria-label="History">
  <div class="hh">
    <b class="display">History</b>
    <Tip text={app.undo ? 'Undo: ' + app.undo : 'Nothing to undo'} kbd="⌘Z"><button class="ib" onclick={undo} disabled={!app.undo} aria-label={app.undo ? 'Undo: ' + app.undo : 'Nothing to undo'}><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 14L4 9l5-5" /><path d="M4 9h11a5 5 0 010 10h-3" /></svg>Undo</button></Tip>
    <Tip text={app.redo ? 'Redo: ' + app.redo : 'Nothing to redo'} kbd="⇧⌘Z"><button class="ib" onclick={redo} disabled={!app.redo} aria-label={app.redo ? 'Redo: ' + app.redo : 'Nothing to redo'}><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M15 14l5-5-5-5" /><path d="M20 9H9a5 5 0 000 10h3" /></svg>Redo</button></Tip>
    <button class="ib" onclick={() => (app.showHistory = false)} aria-label="Close history"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 6L6 18M6 6l12 12" /></svg></button>
  </div>
  <div class="list">
    {#each app.history as h (h.id)}
      {@const w = who(h.actor)}
      <div class="he" class:undone={h.undone}>
        <span class="who {w.c}">{w.t}</span>
        <div><p><b>{w.n}</b> {h.summary}</p><small>{ago(h.at)}{h.undone ? ' · undone' : ''}</small></div>
      </div>
    {:else}
      <p class="muted" style="padding:14px">Nothing yet. Every change you make, and everything the sender does, shows up here.</p>
    {/each}
  </div>
</aside>

<style>
  .hist { position: absolute; top: 0; right: 0; bottom: 0; width: 310px; border-left: 1px solid var(--line); display: flex; flex-direction: column; background: #FBFAF8; box-shadow: -16px 0 40px -24px rgba(17,27,33,.35); transform: translateX(105%); transition: transform .28s var(--ease); z-index: 20 }
  .hist.open { transform: none }
  @media (max-width: 700px) { .hist { width: 100% } }
  .hh { display: flex; align-items: center; gap: 4px; padding: 8px 8px 8px 12px; border-bottom: 1px solid var(--line2) }
  .hh b { font-size: 20px; margin-right: auto }
  .list { overflow: auto; flex: 1; padding: 4px 0 }
  .he { display: grid; grid-template-columns: 22px 1fr; gap: 8px; padding: 8px 12px; font-size: 12.5px; line-height: 1.4 }
  .he:hover { background: #F4F2EE }
  .who { width: 22px; height: 22px; border-radius: 50%; display: grid; place-items: center; font-size: 9.5px; font-weight: 600; color: #fff; background: var(--t800) }
  .who.sys { background: #C9D3CF; color: var(--t900) }
  .who.api { background: #E8DDFB; color: #4B3BA8 }
  p { margin: 0 } p b { font-weight: 600 }
  small { display: block; color: var(--muted); font-size: 11px; margin-top: 2px }
  .undone { opacity: .45 } .undone p { text-decoration: line-through }
</style>
