<script lang="ts">
  // Backing up turns the bar from before-blue to after-green.
  let { value = 0, indeterminate = false, label = '' }: { value?: number; indeterminate?: boolean; label?: string } =
    $props();
  const pct = $derived(Math.max(0, Math.min(100, value)));
</script>

<div
  class="track"
  role="progressbar"
  aria-label={label}
  aria-valuemin="0"
  aria-valuemax="100"
  aria-valuenow={indeterminate ? undefined : Math.round(pct)}
>
  {#if indeterminate}
    <div class="fill indet"></div>
  {:else}
    <div class="fill" style="width: {pct}%; background-size: {pct > 0 ? (100 / pct) * 100 : 100}% 100%"></div>
  {/if}
</div>

<style>
  .track {
    height: 12px;
    border-radius: 999px;
    background: var(--surface-2);
    overflow: hidden;
  }
  .fill {
    height: 100%;
    border-radius: 999px;
    background-image: linear-gradient(90deg, var(--before), var(--after));
    transition: width 240ms var(--ease-out);
  }
  .indet {
    width: 35%;
    background-size: 100% 100%;
    animation: slide 1.3s ease-in-out infinite;
  }
  @keyframes slide {
    from {
      transform: translateX(-100%);
    }
    to {
      transform: translateX(300%);
    }
  }
</style>
