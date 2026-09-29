<script lang="ts">
  import { ChevronRight, Folder, File, RotateCcw, Square, SquareCheck, FolderOpen } from '@lucide/svelte';
  import Mascot from '../lib/ui/Mascot.svelte';
  import Sheet from '../lib/ui/Sheet.svelte';
  import UnlockSheet from '../lib/UnlockSheet.svelte';
  import { api, errText, lang, store, t, toast } from '../lib/store.svelte';
  import { fmtDate, fmtDay, fmtNumber, fmtSize } from '../lib/i18n';
  import type { app } from '../lib/wailsjs/go/models';

  let sources = $state<app.BackupSource[]>([]);
  let sourceId = $state('');
  let backupId = $state('');
  let dir = $state('');
  let listing = $state<app.BrowseResult | null>(null);
  let selected = $state<string[]>([]);
  let restoring = $state<{ paths: string[]; dest: string; date: string } | null>(null);
  let error = $state('');
  let unlocking = $state(false);

  const connected = $derived(!!store.state?.vault?.connected);
  const source = $derived(sources.find((s) => s.id === sourceId));
  const backups = $derived(source?.backups ?? []);
  const backup = $derived(backups.find((b) => b.id === backupId));

  async function load() {
    sources = (await api.ListBackups()) ?? [];
    if (!sources.find((s) => s.id === sourceId)) sourceId = store.historySource || sources[0]?.id || '';
    const bs = sources.find((s) => s.id === sourceId)?.backups ?? [];
    if (!bs.find((b) => b.id === backupId)) backupId = bs.at(-1)?.id ?? '';
  }

  // Reload when the disk's catalog changes (new backups, reconnects).
  $effect(() => {
    void store.state?.vault;
    void store.state?.sources;
    load();
  });

  $effect(() => {
    const [s, b, d] = [sourceId, backupId, dir];
    selected = [];
    if (!s || !b) {
      listing = null;
      return;
    }
    api.Browse(s, b, d).then(
      (r) => (listing = r),
      (e) => (error = errText(e)),
    );
  });

  function pickSource(id: string) {
    sourceId = id;
    backupId = sources.find((s) => s.id === id)?.backups.at(-1)?.id ?? '';
    dir = '';
  }

  function pickBackup(id: string) {
    backupId = id;
  }

  const crumbs = $derived(dir ? dir.split('/') : []);

  function toggle(path: string) {
    selected = selected.includes(path) ? selected.filter((p) => p !== path) : [...selected, path];
  }

  async function startRestore(paths: string[], b: app.BackupInfo) {
    const dest = await api.SuggestRestoreFolder(source?.name ?? 'bleen', String(b.finishedAt));
    restoring = { paths, dest, date: fmtDate(lang(), b.finishedAt) };
  }

  async function changeDest() {
    const p = await api.ChooseFolder(t('restore.where'), '');
    if (p && restoring) restoring.dest = p;
  }

  async function go() {
    if (!restoring || !backup) return;
    try {
      await api.Restore(sourceId, backup.id, restoring.dest, restoring.paths);
      restoring = null;
    } catch (e) {
      toast(errText(e));
    }
  }
</script>

<div class="mx-auto flex h-full max-w-[960px] flex-col px-8 py-8">
  <div class="mb-5 flex items-center gap-3">
    <h1 class="flex-1 font-display text-[26px]">{t('hist.title')}</h1>
    {#if backups.length}
      <button class="btn btn-secondary" disabled={!!store.job} onclick={() => startRestore([], backups.at(-1)!)}>
        <RotateCcw size={16} />{t('hist.latest')}
      </button>
    {/if}
  </div>

  {#if store.state?.vault?.locked}
    <div class="card flex flex-col items-center px-6 py-12 text-center">
      <Mascot mood="sleeping" size={88} />
      <p class="mt-4 text-muted">{t('err.E_PASSWORD_REQUIRED')}</p>
      <button class="btn btn-primary mt-4" onclick={() => (unlocking = true)}>{t('enc.unlock')}</button>
    </div>
  {:else if !connected}
    <div class="card flex flex-col items-center px-6 py-12 text-center">
      <Mascot mood="sleeping" size={88} />
      <p class="mt-4 text-muted">{t('hist.noDisk')}</p>
    </div>
  {:else if !sources.length}
    <div class="card flex flex-col items-center px-6 py-12 text-center">
      <Mascot size={88} />
      <p class="mt-4 text-muted">{t('hist.empty')}</p>
    </div>
  {:else}
    {#if sources.length > 1}
      <div class="mb-4 flex flex-wrap gap-1.5" role="tablist">
        {#each sources as s (s.id)}
          <button
            role="tab"
            aria-selected={s.id === sourceId}
            class="btn h-8 {s.id === sourceId ? 'btn-primary' : 'btn-secondary'}"
            onclick={() => pickSource(s.id)}>{s.name}</button
          >
        {/each}
      </div>
    {/if}

    <!-- Timeline: one dot per backup, oldest → newest -->
    <div class="timeline scroll-thin mb-4 flex gap-1 overflow-x-auto pb-2" role="listbox" aria-label={t('hist.title')}>
      {#each backups as b (b.id)}
        {@const d = fmtDay(lang(), String(b.finishedAt))}
        <button
          role="option"
          aria-selected={b.id === backupId}
          class="day"
          class:sel={b.id === backupId}
          onclick={() => pickBackup(b.id)}
          title={fmtDate(lang(), b.finishedAt)}
        >
          <span class="text-[11px] text-muted">{d.month}</span>
          <span class="dot" class:full={b.kind === 'full'}></span>
          <span class="num text-[15px] font-semibold">{d.day}</span>
          <span class="num text-[11px] text-muted">{d.time}</span>
          <span class="num text-[11px] {b.kind === 'full' ? 'text-accent font-semibold' : 'text-muted'}">
            {b.kind === 'full' ? t('hist.full') : '+' + fmtNumber(lang(), b.new + b.modified)}
          </span>
        </button>
      {/each}
    </div>

    {#if backup}
      <div class="card flex min-h-0 flex-1 flex-col">
        <div class="flex items-center gap-3 border-b border-border px-5 py-4">
          <div class="min-w-0 flex-1">
            <div class="font-semibold">{fmtDay(lang(), String(backup.finishedAt)).weekday} · {fmtDay(lang(), String(backup.finishedAt)).time}</div>
            {#if listing}
              <div class="num text-[13px] text-muted">
                {t('hist.asOf', { files: t('common.files', { n: fmtNumber(lang(), listing.totalFiles) }), size: fmtSize(lang(), listing.totalBytes) })}
              </div>
              {#if source}<div class="truncate font-mono text-[12px] text-muted" title={source.origin}>{source.origin}</div>{/if}
            {/if}
          </div>
          <button class="btn btn-secondary" disabled={!selected.length || !!store.job} onclick={() => startRestore(selected, backup)}>
            {t('hist.selected', { n: selected.length })}
          </button>
          <button class="btn btn-primary" disabled={!!store.job} onclick={() => startRestore([], backup)}>{t('hist.all')}</button>
        </div>

        <nav class="flex items-center gap-1 px-5 pt-3 text-[13px]" aria-label="breadcrumb">
          <button class="crumb" onclick={() => (dir = '')}>{t('hist.root')}</button>
          {#each crumbs as c, i (i)}
            <ChevronRight size={14} class="text-muted" />
            <button class="crumb" onclick={() => (dir = crumbs.slice(0, i + 1).join('/'))}>{c}</button>
          {/each}
        </nav>

        <ul class="scroll-thin min-h-0 flex-1 overflow-auto px-3 py-2">
          {#each listing?.entries ?? [] as e (e.path)}
            <li class="row">
              <button class="check" aria-label={e.name} onclick={() => toggle(e.path)}>
                {#if selected.includes(e.path)}<SquareCheck size={16} class="text-accent" />{:else}<Square size={16} />{/if}
              </button>
              {#if e.dir}
                <button class="name" ondblclick={() => (dir = e.path)} onclick={() => (dir = e.path)}>
                  <Folder size={16} class="text-accent" /><span class="truncate">{e.name}</span>
                </button>
              {:else}
                <span class="name"><File size={16} class="text-muted" /><span class="truncate selectable">{e.name}</span></span>
              {/if}
              <span class="num w-24 text-right text-[12px] text-muted">{fmtSize(lang(), e.size)}</span>
              <span class="num w-36 text-right text-[12px] text-muted">{e.dir ? '' : fmtDate(lang(), e.modTime)}</span>
            </li>
          {/each}
        </ul>
      </div>
    {/if}
    {#if error}<p class="mt-2 text-[13px] text-bad">{error}</p>{/if}
  {/if}
</div>

{#if unlocking}
  <UnlockSheet onclose={() => (unlocking = false)} />
{/if}

{#if restoring}
  <Sheet onclose={() => (restoring = null)}>
    <h2 class="font-display text-[22px]">{t('restore.title')}</h2>
    <p class="mt-1 text-muted">
      {restoring.paths.length
        ? t('restore.what.some', { n: restoring.paths.length, date: restoring.date })
        : t('restore.what.all', { date: restoring.date })}
    </p>
    <label class="mt-5 block text-[13px] font-semibold text-muted" for="dest">{t('restore.where')}</label>
    <div class="mt-1 flex gap-2">
      <input id="dest" class="input font-mono text-[13px]" bind:value={restoring.dest} spellcheck="false" />
      <button class="btn btn-secondary" onclick={changeDest}><FolderOpen size={16} />{t('common.change')}</button>
    </div>
    <p class="mt-2 text-[12px] text-muted">{t('restore.hint')}</p>
    <div class="mt-6 flex justify-end gap-2">
      <button class="btn btn-ghost" onclick={() => (restoring = null)}>{t('common.cancel')}</button>
      <button class="btn btn-primary" onclick={go}>{t('restore.go')}</button>
    </div>
  </Sheet>
{/if}

<style>
  .day {
    flex: none;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 2px;
    width: 64px;
    padding: 8px 4px;
    border-radius: 12px;
    border: 1px solid transparent;
    background: none;
    color: inherit;
    font: inherit;
    cursor: pointer;
    transition: background 120ms;
  }
  .day:hover {
    background: var(--surface-2);
  }
  .day.sel {
    background: var(--surface);
    border-color: var(--accent);
    box-shadow: var(--shadow-sm);
  }
  .dot {
    width: 10px;
    height: 10px;
    border-radius: 99px;
    background: var(--before);
    margin: 3px 0;
  }
  .dot.full {
    background: linear-gradient(90deg, var(--before) 50%, var(--after) 50%);
    width: 14px;
    height: 14px;
    margin: 1px 0;
  }
  .day.sel .dot {
    background: var(--after);
  }
  .crumb {
    padding: 2px 6px;
    border-radius: 6px;
    background: none;
    border: 0;
    color: var(--text);
    font: inherit;
    cursor: pointer;
  }
  .crumb:hover {
    background: var(--surface-2);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 8px;
    border-radius: 8px;
  }
  .row:hover {
    background: var(--surface-2);
  }
  .check {
    display: grid;
    place-items: center;
    background: none;
    border: 0;
    color: var(--muted);
    cursor: pointer;
    padding: 2px;
  }
  .name {
    display: flex;
    flex: 1;
    min-width: 0;
    align-items: center;
    gap: 8px;
    background: none;
    border: 0;
    color: inherit;
    font: inherit;
    text-align: left;
    padding: 4px 0;
  }
  button.name {
    cursor: pointer;
  }
</style>
