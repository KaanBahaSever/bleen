<script lang="ts">
  import { FolderOpen, Server } from '@lucide/svelte';
  import { api, errText, t } from './store.svelte';

  let { onadded }: { onadded?: () => void } = $props();
  let path = $state('');
  let busy = $state(false);
  let error = $state('');

  async function pick() {
    const p = await api.ChooseFolder(t('add.pick'), '');
    if (p) {
      path = p;
      await add();
    }
  }

  async function add() {
    error = '';
    busy = true;
    try {
      await api.AddSource(path);
      path = '';
      onadded?.();
    } catch (e) {
      error = errText(e);
    } finally {
      busy = false;
    }
  }
</script>

<form
  class="flex flex-col gap-3"
  onsubmit={(e) => {
    e.preventDefault();
    add();
  }}
>
  <div class="flex gap-2">
    <div class="relative flex-1">
      <Server size={16} class="absolute left-3 top-1/2 -translate-y-1/2 text-muted" />
      <input
        class="input font-mono text-[13px]"
        style="padding-left: 34px"
        placeholder={t('add.placeholder')}
        bind:value={path}
        spellcheck="false"
        autocomplete="off"
        aria-label={t('add.title')}
      />
    </div>
    <button class="btn btn-primary" type="submit" disabled={busy || !path.trim()}>
      {busy ? t('add.checking') : t('common.add')}
    </button>
  </div>
  <div>
    <button class="btn btn-secondary" type="button" onclick={pick} disabled={busy}>
      <FolderOpen size={16} />
      {t('common.browse')}
    </button>
  </div>
  {#if error}
    <p class="text-[13px] text-bad" role="alert">{error}</p>
  {/if}
</form>
