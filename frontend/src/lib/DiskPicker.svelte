<script lang="ts">
  import { HardDrive, Usb, FolderOpen, TriangleAlert } from '@lucide/svelte';
  import { onMount } from 'svelte';
  import type { platform } from './wailsjs/go/models';
  import { api, errText, lang, t } from './store.svelte';
  import { fmtSize } from './i18n';

  let { onchosen }: { onchosen?: () => void } = $props();
  let drives = $state<platform.Volume[]>([]);
  let busy = $state(false);
  let error = $state('');
  let encrypt = $state(false);
  let pw = $state('');
  let pw2 = $state('');

  onMount(async () => {
    drives = (await api.Drives()) ?? [];
  });

  async function use(dir: string) {
    error = '';
    if (encrypt && pw.length < 8) return void (error = t('err.E_WEAK_PASSWORD'));
    if (encrypt && pw !== pw2) return void (error = t('enc.mismatch'));
    busy = true;
    try {
      await api.UseVaultFolder(dir, encrypt ? pw : '');
      onchosen?.();
    } catch (e) {
      error = errText(e);
    } finally {
      busy = false;
    }
  }

  async function other() {
    const p = await api.ChooseFolder(t('onb.vault.other'), '');
    if (p) await use(p);
  }

  const isFat = (fs: string) => ['fat32', 'fat', 'vfat', 'msdos'].includes(fs.toLowerCase());
</script>

<div class="flex flex-col gap-2">
  <label class="mb-2 flex items-start gap-3 rounded-xl bg-surface-2 px-4 py-3">
    <input type="checkbox" class="mt-1" bind:checked={encrypt} />
    <span class="flex-1">
      <span class="block font-semibold">{t('enc.toggle')}</span>
      <span class="block text-[12px] text-muted">{t('enc.hint')}</span>
      {#if encrypt}
        <span class="mt-3 flex gap-2">
          <input class="input" type="password" autocomplete="new-password" placeholder={t('enc.password')} aria-label={t('enc.password')} bind:value={pw} />
          <input class="input" type="password" autocomplete="new-password" placeholder={t('enc.repeat')} aria-label={t('enc.repeat')} bind:value={pw2} />
        </span>
        <span class="mt-2 block text-[12px] font-semibold text-warn">{t('enc.warn')}</span>
      {/if}
    </span>
  </label>
  {#each drives as d (d.path)}
    <button class="drive card" disabled={busy} onclick={() => use(d.path)}>
      <span class="icon">
        {#if d.removable}<Usb size={20} />{:else}<HardDrive size={20} />{/if}
      </span>
      <span class="flex-1 text-left">
        <span class="block font-semibold">{d.label || d.path}{#if d.label} <span class="text-muted font-normal">{d.path}</span>{/if}</span>
        <span class="block text-[12px] text-muted num">
          {t('common.free', { size: fmtSize(lang(), d.free) })} · {d.fsType}
        </span>
        {#if isFat(d.fsType)}
          <span class="mt-1 flex items-start gap-1.5 text-[12px] text-warn">
            <TriangleAlert size={14} class="mt-[2px] flex-none" />{t('onb.vault.fat32')}
          </span>
        {/if}
      </span>
      <span class="bar" aria-hidden="true">
        <span style="width: {d.total ? Math.round(((d.total - d.free) / d.total) * 100) : 0}%"></span>
      </span>
    </button>
  {:else}
    <p class="text-muted text-[13px]">{t('onb.vault.none')}</p>
  {/each}
  <div class="mt-1">
    <button class="btn btn-secondary" onclick={other} disabled={busy}>
      <FolderOpen size={16} />{t('onb.vault.other')}
    </button>
  </div>
  {#if error}<p class="text-[13px] text-bad" role="alert">{error}</p>{/if}
</div>

<style>
  .drive {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 14px 16px;
    cursor: pointer;
    transition: transform 120ms var(--ease-out), border-color 120ms;
    color: var(--text);
    font: inherit;
  }
  .drive:hover {
    border-color: var(--accent);
    transform: translateY(-1px);
  }
  .icon {
    width: 40px;
    height: 40px;
    border-radius: 12px;
    display: grid;
    place-items: center;
    background: var(--accent-soft);
    color: var(--accent);
  }
  .bar {
    width: 72px;
    height: 6px;
    border-radius: 99px;
    background: var(--surface-2);
    overflow: hidden;
  }
  .bar span {
    display: block;
    height: 100%;
    background: var(--before);
  }
</style>
