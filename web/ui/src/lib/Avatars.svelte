<script>
  import { avatar } from './state.svelte.js'
  let { jids = [], max = 4, size = 18 } = $props()
</script>

<span class="avs">
  {#each jids.slice(0, max) as j (j)}
    {@const a = avatar(j)}
    <span class="av" title={a.name} style="background:{a.bg};width:{size}px;height:{size}px;font-size:{size * 0.45}px">{a.ini}</span>
  {/each}
  {#if jids.length > max}<span class="av more" style="width:{size}px;height:{size}px;font-size:{size * 0.42}px">+{jids.length - max}</span>{/if}
</span>

<style>
  .more { background: #DCE3E0; color: var(--ink2) }
</style>
