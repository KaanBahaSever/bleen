<script lang="ts">
  import type { Snippet } from 'svelte';
  let {
    open = true,
    onclose,
    width = 520,
    children,
  }: { open?: boolean; onclose?: () => void; width?: number; children: Snippet } = $props();

  // Render at <body>: an animated ancestor would otherwise become the
  // containing block for position: fixed and the backdrop would not cover the window.
  function portal(node: HTMLElement) {
    document.body.appendChild(node);
    return { destroy: () => node.remove() };
  }

  function key(e: KeyboardEvent) {
    if (e.key === 'Escape' && onclose) onclose();
  }
</script>

<svelte:window onkeydown={key} />

{#if open}
  <div use:portal>
    <div class="backdrop" role="presentation" onclick={() => onclose?.()}></div>
    <div class="sheet card fade-up" role="dialog" aria-modal="true" style="width: min({width}px, calc(100vw - 48px))">
      {@render children()}
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgb(18 20 23 / 0.28);
    backdrop-filter: blur(6px);
    z-index: 40;
  }
  .sheet {
    position: fixed;
    left: 50%;
    top: 50%;
    translate: -50% -50%;
    z-index: 50;
    max-height: calc(100vh - 64px);
    overflow: auto;
    border-radius: 16px;
    box-shadow: var(--shadow-md);
    padding: 28px;
  }
</style>
