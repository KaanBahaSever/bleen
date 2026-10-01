<script lang="ts">
  import Sheet from './Sheet.svelte';
  import { t } from '../store.svelte';

  let {
    title,
    body,
    confirm,
    onconfirm,
    onclose,
  }: { title: string; body: string; confirm: string; onconfirm: () => void | Promise<void>; onclose: () => void } = $props();

  let busy = $state(false);
  async function yes() {
    busy = true;
    try {
      await onconfirm();
    } finally {
      busy = false;
      onclose();
    }
  }
</script>

<Sheet {onclose} width={440}>
  <h2 class="font-display text-[20px]">{title}</h2>
  <p class="mt-2 text-muted">{body}</p>
  <div class="mt-6 flex justify-end gap-2">
    <button class="btn btn-ghost" onclick={onclose}>{t('common.cancel')}</button>
    <button class="btn btn-danger" disabled={busy} onclick={yes}>{confirm}</button>
  </div>
</Sheet>
