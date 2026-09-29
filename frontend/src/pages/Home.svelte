<script lang="ts">
  import { HardDrive, Plus, Ellipsis, Folder, Server, TriangleAlert } from '@lucide/svelte';
  import Mascot from '../lib/ui/Mascot.svelte';
  import Sparkline from '../lib/ui/Sparkline.svelte';
  import Sheet from '../lib/ui/Sheet.svelte';
  import AddSource from '../lib/AddSource.svelte';
  import DiskPicker from '../lib/DiskPicker.svelte';
  import { api, backupNow, lang, store, t } from '../lib/store.svelte';
  import { fmtRelative, fmtSize, isZeroTime } from '../lib/i18n';
  import type { app } from '../lib/wailsjs/go/models';

  const st = $derived(store.state);
  const sources = $derived(st?.sources ?? []);
  const vault = $derived(st?.vault ?? null);
  let adding = $state(false);
  let picking = $state(false);
  let menu = $state('');

  const primary = $derived.by(() => {
    if (!sources.length) return { label: t('home.btn.addFirst'), action: () => (adding = true), disabled: false };
    if (!vault) return { label: t('home.btn.chooseDisk'), action: () => (picking = true), disabled: false };
    if (!vault.connected)
      return { label: t('home.btn.plugIn', { label: vault.label || vault.path }), action: () => {}, disabled: true };
    const needsFirst = sources.some((s) => s.backups === 0);
    return {
      label: needsFirst ? t('home.btn.first') : t('home.btn.changes'),
      action: () => backupNow(),
      disabled: !!store.job,
    };
  });

  const lastBackup = $derived(
    sources
      .map((s) => s.lastBackup)
      .filter((d) => !isZeroTime(d))
      .sort()
      .at(-1),
  );

  function status(s: app.SourceState): { cls: string; text: string } {
    if (s.reachable === false) return { cls: 'pill-warn', text: t('src.unreachable') };
    if (s.backups === 0) return { cls: 'pill-info', text: t('src.never') };
    if (s.lastIssues > 0) return { cls: 'pill-warn', text: t('src.issues', { n: s.lastIssues }) };
    const days = (Date.now() - new Date(s.lastBackup).getTime()) / 86400000;
    const when = fmtRelative(lang(), s.lastBackup);
    if (days > 7) return { cls: 'pill-warn', text: t('src.stale', { when }) };
    return { cls: 'pill-ok', text: t('src.upToDate', { when }) };
  }

  function openHistory(s: app.SourceState) {
    if (!s.vaultSourceId) return;
    store.historySource = s.vaultSourceId;
    store.route = 'history';
  }
</script>

<svelte:window onclick={() => (menu = '')} />

<div class="mx-auto flex max-w-[880px] flex-col gap-7 px-8 py-8">
  <!-- Backup disk -->
  <section>
    <h2 class="mb-2 text-[13px] font-semibold text-muted">{t('home.disk')}</h2>
    {#if vault}
      <div class="card flex items-center gap-4 px-5 py-4">
        <span class="grid h-10 w-10 place-items-center rounded-xl bg-accent-soft text-accent"><HardDrive size={20} /></span>
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2">
            <span class="font-semibold">{vault.label || vault.path}</span>
            {#if vault.connected}
              <span class="h-2 w-2 rounded-full bg-ok" aria-hidden="true"></span>
            {/if}
          </div>
          {#if vault.connected}
            <div class="truncate font-mono text-[12px] text-muted">{vault.path}</div>
          {:else}
            <div class="text-[13px] text-warn">{t('home.diskMissing', { label: vault.label || vault.path })}</div>
          {/if}
        </div>
        {#if vault.connected}
          <span class="num text-[13px] text-muted">{t('common.free', { size: fmtSize(lang(), vault.free) })}</span>
        {/if}
        <button class="btn btn-ghost" onclick={() => (picking = true)}>{t('common.change')}</button>
      </div>
    {:else}
      <div class="card flex items-center gap-4 px-5 py-4">
        <p class="flex-1 text-muted">{t('home.noDisk')}</p>
        <button class="btn btn-secondary" onclick={() => (picking = true)}>{t('home.chooseDisk')}</button>
      </div>
    {/if}
  </section>

  <!-- Locations -->
  <section>
    <div class="mb-2 flex items-center">
      <h2 class="flex-1 text-[13px] font-semibold text-muted">{t('home.locations')}</h2>
      <button class="btn btn-ghost h-8" onclick={() => (adding = true)}><Plus size={16} />{t('home.addLocation')}</button>
    </div>
    {#if sources.length === 0}
      <div class="card flex flex-col items-center px-6 py-10 text-center">
        <Mascot mood="sleeping" size={96} />
        <p class="mt-4 font-semibold">{t('home.empty.title')}</p>
        <p class="mt-1 text-muted">{t('home.empty.body')}</p>
      </div>
    {:else}
      <div class="grid grid-cols-2 gap-3">
        {#each sources as s (s.id)}
          {@const stt = status(s)}
          <div
            class="source card relative flex flex-col gap-3 px-5 py-4"
            role="button"
            tabindex="0"
            onclick={() => openHistory(s)}
            onkeydown={(e) => e.key === 'Enter' && openHistory(s)}
          >
            <div class="flex items-start gap-3">
              <span class="mt-0.5 text-accent">
                {#if s.path.startsWith('\\\\')}<Server size={18} />{:else}<Folder size={18} />{/if}
              </span>
              <div class="min-w-0 flex-1">
                <div class="truncate font-semibold">{s.name}</div>
                <div class="truncate font-mono text-[12px] text-muted" title={s.path}>{s.path}</div>
              </div>
              <button
                class="btn btn-ghost h-7 w-7 p-0"
                aria-label="…"
                onclick={(e) => {
                  e.stopPropagation();
                  menu = menu === s.id ? '' : s.id;
                }}><Ellipsis size={16} /></button
              >
              {#if menu === s.id}
                <div class="menu card" role="menu">
                  <button role="menuitem" disabled={!vault?.connected || !!store.job} onclick={() => backupNow([s.id])}>
                    {t('home.backupOnly')}
                  </button>
                  <button role="menuitem" class="text-bad" onclick={() => api.RemoveSource(s.id)}>{t('common.remove')}</button>
                </div>
              {/if}
            </div>
            <div class="flex items-end justify-between gap-2">
              <span class="pill {stt.cls}">
                {#if stt.cls === 'pill-warn'}<TriangleAlert size={12} />{/if}
                {stt.text}
              </span>
              <Sparkline values={s.recent ?? []} label={t('src.backups', { n: s.backups })} />
            </div>
          </div>
        {/each}
      </div>
    {/if}
  </section>

  <!-- Primary action -->
  <section class="flex flex-col items-center gap-2 pt-2">
    <button class="btn btn-primary btn-lg min-w-[260px]" disabled={primary.disabled} onclick={primary.action}>
      {primary.label}
    </button>
    {#if lastBackup}
      <p class="text-[13px] text-muted">{t('home.lastRun', { when: fmtRelative(lang(), lastBackup) })}</p>
    {/if}
  </section>
</div>

{#if adding}
  <Sheet onclose={() => (adding = false)}>
    <h2 class="mb-1 font-display text-[22px]">{t('add.title')}</h2>
    <p class="mb-4 text-muted">{t('onb.source.body')}</p>
    <AddSource onadded={() => (adding = false)} />
  </Sheet>
{/if}

{#if picking}
  <Sheet onclose={() => (picking = false)}>
    <h2 class="mb-1 font-display text-[22px]">{t('onb.vault.title')}</h2>
    <p class="mb-4 text-muted">{t('onb.vault.body')}</p>
    <DiskPicker onchosen={() => (picking = false)} />
  </Sheet>
{/if}

<style>
  .source {
    cursor: pointer;
    transition: transform 120ms var(--ease-out), box-shadow 120ms;
  }
  .source:hover {
    transform: translateY(-1px);
    box-shadow: var(--shadow-md);
  }
  .menu {
    position: absolute;
    right: 12px;
    top: 44px;
    z-index: 10;
    display: flex;
    flex-direction: column;
    padding: 6px;
    min-width: 200px;
    box-shadow: var(--shadow-md);
  }
  .menu button {
    text-align: left;
    padding: 8px 10px;
    border-radius: 8px;
    background: none;
    border: 0;
    color: inherit;
    font: inherit;
    cursor: pointer;
  }
  .menu button:hover:not(:disabled) {
    background: var(--surface-2);
  }
  .menu button:disabled {
    opacity: 0.5;
  }
</style>
