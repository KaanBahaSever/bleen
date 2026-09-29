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

  onMount(async () => {
    drives = (await api.Drives()) ?? [];
  });

  async function use(dir: string) {
    error = '';
    busy = true;
    try {
      await api.UseVaultFolder(dir);
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
