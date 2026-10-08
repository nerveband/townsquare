<script>
  // A filter list of checkboxes with All / None and an "only" shortcut on hover.
  // `hidden` holds the ids that are unchecked (0 = the "none" item).
  let { label, items = [], hidden = $bindable([]), noneLabel = '', counts = {} } = $props()
  const all = $derived([...items.map((i) => i.id), ...(noneLabel ? [0] : [])])
  function toggle(id) { hidden = hidden.includes(id) ? hidden.filter((x) => x !== id) : [...hidden, id] }
  function only(id) { hidden = all.filter((x) => x !== id) }
</script>

<div class="cl">
  <div class="hd">
    <span class="label">{label}</span>
    <span class="qa">
      <button onclick={() => (hidden = [])} disabled={!hidden.length}>All</button>
      <button onclick={() => (hidden = [...all])} disabled={hidden.length === all.length}>None</button>
    </span>
  </div>
  {#each [...items, ...(noneLabel ? [{ id: 0, name: noneLabel, color: '#9AA5A1' }] : [])] as it (it.id)}
    <label class="row" class:off={hidden.includes(it.id)}>
      <input type="checkbox" checked={!hidden.includes(it.id)} onchange={() => toggle(it.id)} style="--c:{it.color}" />
      <span class="nm">{it.name}</span>
      {#if counts[it.id]}<span class="n">{counts[it.id]}</span>{/if}
      <button class="only" onclick={(e) => { e.preventDefault(); only(it.id) }} title="Show only {it.name}">only</button>
    </label>
  {/each}
</div>

<style>
  .cl { display: flex; flex-direction: column; gap: 1px }
  .hd { display: flex; align-items: center; justify-content: space-between; margin: 0 4px 3px }
  .qa { display: flex; gap: 2px }
  .qa button { border: 0; background: transparent; font-size: 11px; color: var(--t600); padding: 1px 4px; border-radius: 4px }
  .qa button:hover:not(:disabled) { background: var(--sunk) }
  .qa button:disabled { color: #B9BFC2; cursor: default }
  .row { display: flex; align-items: center; gap: 7px; padding: 3px 6px; border-radius: 6px; font-size: 12.5px; color: var(--ink2); cursor: pointer }
  .row:hover { background: var(--sunk) }
  .row.off .nm { color: #A3ABAE }
  input { appearance: none; width: 14px; height: 14px; border-radius: 4px; border: 1.5px solid var(--c); margin: 0; flex: none; display: grid; place-items: center; cursor: pointer }
  input:checked { background: var(--c) }
  input:checked::after { content: ""; width: 7px; height: 4px; border: 2px solid #fff; border-top: 0; border-right: 0; transform: rotate(-45deg) translate(1px, -1px) }
  .nm { flex: 1; white-space: nowrap; overflow: hidden; text-overflow: ellipsis }
  .n { font: 500 10.5px var(--mono); color: var(--muted) }
  .only { border: 0; background: transparent; font-size: 11px; color: var(--t600); padding: 0 3px; border-radius: 4px; opacity: 0 }
  .row:hover .only { opacity: 1 }
  .row:hover .n { display: none }
  @media (hover: none) { .only { opacity: 1 } }
</style>
