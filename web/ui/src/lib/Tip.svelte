<script>
  // Small hover/focus tooltip that shows instantly (native title tooltips are slow and easy to miss).
  let { text = '', kbd = '', place = 'below', children } = $props()
</script>

<span class="tipwrap">
  {@render children()}
  {#if text}<span class="tip {place}" role="tooltip">{text}{#if kbd}<kbd>{kbd}</kbd>{/if}</span>{/if}
</span>

<style>
  .tipwrap { position: relative; display: inline-flex }
  .tip { position: absolute; z-index: 700; right: 0; top: calc(100% + 6px); width: max-content; max-width: 260px; background: var(--ink); color: #fff; font-size: 12px; line-height: 1.35; padding: 6px 8px; border-radius: 7px; box-shadow: 0 8px 20px rgba(0,0,0,.2); opacity: 0; transform: translateY(-2px); pointer-events: none; transition: opacity .12s, transform .12s }
  .tip.above { top: auto; bottom: calc(100% + 6px) }
  .tipwrap:hover .tip, .tipwrap:focus-within .tip { opacity: 1; transform: none }
  kbd { font: 500 10.5px var(--mono); margin-left: 6px; padding: 0 4px; border: 1px solid rgba(255,255,255,.3); border-radius: 4px; color: #D3E2DE }
  @media (hover: none) { .tip { display: none } }
</style>
