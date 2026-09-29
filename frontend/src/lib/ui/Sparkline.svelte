<script lang="ts">
  // Tiny bar chart of the bytes stored by the last backups (oldest → newest).
  let { values = [] as number[], label = '' }: { values?: number[]; label?: string } = $props();
  const max = $derived(Math.max(1, ...values));
</script>

{#if values.length > 0}
  <div class="spark" role="img" aria-label={label}>
    {#each values as v, i (i)}
      <span style="height: {Math.max(8, (v / max) * 100)}%" class:last={i === values.length - 1}></span>
    {/each}
  </div>
{/if}

<style>
  .spark {
    display: flex;
    align-items: flex-end;
    gap: 3px;
    height: 22px;
  }
  span {
    width: 6px;
    border-radius: 2px;
    background: var(--before);
    opacity: 0.55;
  }
  span.last {
    background: var(--after);
    opacity: 1;
  }
</style>
