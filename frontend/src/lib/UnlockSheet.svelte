<script lang="ts">
  import { LockOpen } from '@lucide/svelte';
  import Sheet from './ui/Sheet.svelte';
  import { api, errText, store, t } from './store.svelte';

  let { onclose }: { onclose: () => void } = $props();
  let password = $state('');
  let busy = $state(false);
  let error = $state('');
  const label = $derived(store.state?.vault?.label || store.state?.vault?.path || '');

  // The sheet is moved to <body>, where the autofocus attribute is ignored.
  function focus(node: HTMLInputElement) {
    setTimeout(() => node.focus(), 60);
  }

  async function unlock(e: Event) {
    e.preventDefault();
    busy = true;
    error = '';
    try {
      await api.Unlock(password);
      onclose();
    } catch (err) {
      error = errText(err);
    } finally {
      busy = false;
    }
  }
</script>

<Sheet {onclose} width={440}>
  <form onsubmit={unlock}>
    <h2 class="font-display text-[22px]">{t('enc.unlockTitle', { label })}</h2>
    <p class="mt-1 text-muted">{t('enc.unlockBody')}</p>
    <input class="input mt-5" type="password" autocomplete="current-password" aria-label={t('enc.password')}
      placeholder={t('enc.password')} bind:value={password} use:focus />
    {#if error}<p class="mt-2 text-[13px] text-bad" role="alert">{error}</p>{/if}
    <div class="mt-6 flex justify-end gap-2">
      <button type="button" class="btn btn-ghost" onclick={onclose}>{t('common.cancel')}</button>
      <button class="btn btn-primary" disabled={busy || !password}><LockOpen size={16} />{t('enc.unlock')}</button>
    </div>
  </form>
</Sheet>
