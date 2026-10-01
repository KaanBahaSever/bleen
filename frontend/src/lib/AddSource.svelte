<script lang="ts">
  import { FolderOpen, Server } from '@lucide/svelte';
  import { api, errText, store, t } from './store.svelte';

  let { onadded }: { onadded?: () => void } = $props();
  let path = $state('');
  let busy = $state(false);
  let error = $state('');
  let code = $state('');
  let user = $state('');
  let pass = $state('');
  // Offer "connect as" when a Windows network path is refused or unreachable.
  const canConnectAs = $derived(
    store.state?.os === 'windows' &&
      /^(\\\\|\/\/)/.test(path.trim()) &&
      (code === 'E_ACCESS_DENIED' || code === 'E_SOURCE_UNREACHABLE'),
  );

  async function pick() {
    const p = await api.ChooseFolder(t('add.pick'), '');
    if (p) {
      path = p;
      await add();
    }
  }

  async function add(withCredentials = false) {
    error = '';
    busy = true;
    try {
      if (withCredentials) await api.AddSourceAs(path, user, pass);
      else await api.AddSource(path);
      path = user = pass = code = '';
      onadded?.();
    } catch (e) {
      error = errText(e);
      code = String((e as Error)?.message ?? e).match(/E_[A-Z_]+/)?.[0] ?? '';
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
  {#if canConnectAs}
    <div class="rounded-xl bg-surface-2 p-4">
      <p class="mb-2 font-semibold">{t('add.as')}</p>
      <div class="flex gap-2">
        <input class="input" autocomplete="username" placeholder={t('add.user')} aria-label={t('add.user')} bind:value={user} />
        <input class="input" type="password" autocomplete="current-password" placeholder={t('add.pass')} aria-label={t('add.pass')} bind:value={pass} />
      </div>
      <p class="mt-2 text-[12px] text-muted">{t('add.asHint')}</p>
      <button class="btn btn-primary mt-3" type="button" disabled={busy || !user || !pass} onclick={() => add(true)}>{t('add.connect')}</button>
    </div>
  {/if}
</form>
