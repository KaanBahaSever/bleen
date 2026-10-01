<script lang="ts">
  import { Check, TriangleAlert, X, CircleAlert, Pause, Play } from '@lucide/svelte';
  import Sheet from './ui/Sheet.svelte';
  import Mascot from './ui/Mascot.svelte';
  import ProgressBean from './ui/ProgressBean.svelte';
  import { api, errText, issueText, lang, store, t } from './store.svelte';
  import { fmtDate, fmtDuration, fmtNumber, fmtSize, isZeroTime } from './i18n';
  import type { app } from './wailsjs/go/models';

  const job = $derived(store.job!);
  const p = $derived(job.progress);
  const pct = $derived(p && p.bytesTotal > 0 ? (p.bytesDone / p.bytesTotal) * 100 : p?.filesTotal ? (p.filesDone / p.filesTotal) * 100 : 0);
  const finished = $derived(['done', 'failed', 'cancelled'].includes(job.phase));
  let showIssues = $state(false);
  // One answer per preflight: a double click must not approve the next plan.
  let answeredPlan = $state<unknown>(null);
  function answer(ok: boolean) {
    answeredPlan = job.plan;
    api.ConfirmPlan(ok);
  }
  async function breakLock() {
    await api.BreakLock();
    api.DismissJob();
  }

  // ETA from throughput since the copy phase started.
  let startedAt = 0;
  let startedBytes = 0;
  let eta = $state('');
  $effect(() => {
    if (job.phase !== 'running' || !p) {
      startedAt = 0;
      eta = '';
      return;
    }
    if (!startedAt) {
      startedAt = performance.now();
      startedBytes = p.bytesDone;
      return;
    }
    const secs = (performance.now() - startedAt) / 1000;
    const rate = (p.bytesDone - startedBytes) / secs;
    if (secs > 3 && rate > 0) eta = fmtDuration(lang(), (p.bytesTotal - p.bytesDone) / rate);
  });

  const mood = $derived(
    job.phase === 'failed'
      ? 'concerned'
      : job.phase === 'done'
        ? (job.results ?? []).some((r) => r.result === 'failed' || (r.issues?.length ?? 0) > 0)
          ? 'concerned'
          : 'happy'
        : job.phase === 'cancelled' || job.phase === 'awaiting'
          ? 'idle'
          : job.paused
            ? 'sleeping'
            : 'working',
  );

  const title = $derived.by(() => {
    if (job.phase === 'done') return t('run.done');
    if (job.phase === 'failed') return t('run.failed');
    if (job.phase === 'cancelled') return t('run.cancelled');
    if (job.phase === 'awaiting') return t('run.preflight');
    if (job.kind === 'restore') return t('run.restoring', { name: job.sourceName });
    if (job.kind === 'verify') return t('run.verifyingAll');
    return t('run.backingUp', { name: job.sourceName || '…' });
  });

  const allIssues = $derived((job.results ?? []).flatMap((r) => (r.issues ?? []).map((i) => ({ ...i, source: r.source }))));

  function close() {
    if (finished) api.DismissJob();
  }

  function row(label: string, value: string) {
    return { label, value };
  }

  function planRows(pl: NonNullable<app.JobState['plan']>) {
    const rows = [];
    if (pl.kind === 'incremental') {
      if (!isZeroTime(pl.lastFull)) rows.push(row(t('run.lastFull'), fmtDate(lang(), pl.lastFull)));
      if (!isZeroTime(pl.lastBackup)) rows.push(row(t('run.lastBackup'), fmtDate(lang(), pl.lastBackup)));
      rows.push(row(t('run.new'), fmtNumber(lang(), pl.new)));
      rows.push(row(t('run.changed'), fmtNumber(lang(), pl.modified)));
      rows.push(row(t('run.deleted'), fmtNumber(lang(), pl.deleted)));
    } else {
      rows.push(row(t('run.total'), fmtNumber(lang(), pl.totalFiles)));
    }
    rows.push(row(t('run.estimate'), '~' + fmtSize(lang(), pl.estStored)));
    if (pl.vaultFree) rows.push(row(t('run.diskFree'), fmtSize(lang(), pl.vaultFree)));
    if (pl.scanIssues) rows.push(row(t('run.unreadable'), fmtNumber(lang(), pl.scanIssues)));
    return rows;
  }
</script>

<Sheet onclose={close} width={560}>
  <div class="flex items-center gap-4">
    <Mascot {mood} size={64} />
    <div class="min-w-0 flex-1">
      <h2 class="font-display text-[22px] leading-7">{title}</h2>
      {#if job.count > 1 && !finished}
        <p class="text-[13px] text-muted">{t('run.of', { i: job.index, n: job.count })} · {job.sourceName}</p>
      {:else if job.phase === 'awaiting' && job.plan}
        <p class="truncate font-mono text-[12px] text-muted">{job.plan.sourceName} ← {job.plan.origin}</p>
      {/if}
    </div>
  </div>

  <div class="mt-6">
    {#if job.phase === 'scanning'}
      <ProgressBean indeterminate label={t('run.scanning')} />
      <p class="num mt-3 text-muted">{t('run.scanning')} {t('run.scanned', { n: fmtNumber(lang(), job.scanned) })}</p>
    {:else if job.phase === 'awaiting' && job.plan}
      {@const pl = job.plan}
      {#if pl.massChange}
        <div class="mb-4 rounded-xl bg-warn-bg p-4 text-warn">
          <div class="flex items-center gap-2 font-semibold"><TriangleAlert size={16} />{t('run.mass.title')}</div>
          <p class="mt-1 text-[13px]">{t('run.mass.body', { pct: Math.round(pl.changedRatio * 100) })}</p>
        </div>
      {/if}
      <p class="mb-2 text-[13px] font-semibold text-muted">{pl.kind === 'full' ? t('run.full') : t('run.incremental')}</p>
      <dl class="card divide-y divide-border px-4">
        {#each planRows(pl) as r (r.label)}
          <div class="flex justify-between py-2.5">
            <dt class="text-muted">{r.label}</dt>
            <dd class="num font-semibold">{r.value}</dd>
          </div>
        {/each}
      </dl>
    {:else if job.phase === 'running'}
      {#if job.paused}<p class="mb-3 text-[13px] font-semibold text-warn">{t('run.paused')}</p>{/if}
      <ProgressBean value={pct} label={title} />
      <div class="num mt-3 flex justify-between text-[13px] text-muted">
        <span>{t('run.progress', { done: fmtNumber(lang(), p.filesDone), total: fmtNumber(lang(), p.filesTotal) })} · {Math.round(pct)}%</span>
        {#if eta}<span>{t('run.left', { t: eta })}</span>{/if}
      </div>
      <p class="mt-2 truncate font-mono text-[12px] text-muted" title={p.current}>{p.current}</p>
    {:else if job.phase === 'verifying' || job.phase === 'saving'}
      <ProgressBean value={job.kind === 'verify' ? pct : 100} indeterminate={job.kind !== 'verify'} label={title} />
      <p class="mt-3 text-muted">{job.phase === 'verifying' ? t('run.verifying') : t('run.saving')}</p>
      {#if job.kind === 'verify' && p?.current}<p class="mt-1 truncate font-mono text-[12px] text-muted">{p.current}</p>{/if}
    {:else if finished}
      {#if job.phase === 'failed' && job.error && !(job.results ?? []).length}
        <div class="rounded-xl bg-bad-bg p-4 text-bad">
          <p class="font-semibold">{job.error.code === 'E_CHECKSUM_MISMATCH' ? t('run.newArchiveBad') : errText(job.error.code)}</p>
          {#if job.error.code === 'E_VAULT_LOCKED'}
            <button class="btn btn-secondary mt-3" onclick={breakLock}>{t('run.breakLock')}</button>
            <p class="mt-1 text-[12px] opacity-80">{t('run.breakLock.hint')}</p>
          {/if}
          <p class="selectable mt-1 text-[12px] opacity-80">{job.error.message}</p>
        </div>
      {/if}
      <ul class="flex flex-col gap-3">
        {#each job.results ?? [] as r (r.id)}
          <li>
            {#if r.kind === 'backup'}
              {#if r.result === 'done'}
                {#if job.count > 1}<p class="mb-1 font-semibold">{r.source}</p>{/if}
                <p class="check"><Check size={16} />{t('run.ok.files', { n: fmtNumber(lang(), r.files) })}</p>
                {#if r.deduped}<p class="ml-6 text-[12px] text-muted">{t('run.ok.dedup', { n: fmtNumber(lang(), r.deduped) })}</p>{/if}
                <p class="check"><Check size={16} />{t('run.ok.size', { size: fmtSize(lang(), r.stored) })}</p>
                {#if r.verified}<p class="check"><Check size={16} />{t('run.ok.verified')}</p>{/if}
                <p class="check"><Check size={16} />{t('run.ok.catalog')}</p>
                {#if r.pruned}<p class="check"><Check size={16} />{t('run.ok.pruned', { n: r.pruned })}</p>{/if}
              {:else if r.result === 'nothing'}
                <p class="check"><Check size={16} />{t('run.nothing', { name: r.source })}</p>
              {:else if r.result === 'cancelled'}
                <p class="flex items-center gap-2 text-muted"><X size={16} />{r.source}: {t('act.cancelled')}</p>
              {:else}
                <div class="rounded-xl bg-bad-bg p-3 text-bad">
                  <p class="flex items-center gap-2 font-semibold"><CircleAlert size={16} />{r.source}</p>
                  <p class="mt-1 text-[13px]">{errText(r.error?.code)}</p>
                </div>
              {/if}
            {:else if r.kind === 'restore'}
              {#if r.result === 'done'}
                <p class="check"><Check size={16} />{t('run.restored', { n: fmtNumber(lang(), r.files) })} ({fmtSize(lang(), r.bytes)})</p>
                <p class="ml-6 truncate font-mono text-[12px] text-muted">{t('run.restoredTo', { dest: r.dest ?? '' })}</p>
                {#if r.verified}
                  <p class="check"><Check size={16} />{t('run.allVerified')}</p>
                {:else}
                  <p class="flex items-center gap-2 font-semibold text-bad"><CircleAlert size={16} />{t('run.restoreIssues', { n: r.issues?.length ?? 0 })}</p>
                {/if}
              {:else}
                <div class="rounded-xl bg-bad-bg p-3 text-bad"><p class="text-[13px]">{errText(r.error?.code)}</p></div>
              {/if}
            {:else if r.kind === 'verify'}
              {#if r.result === 'done' && !(r.issues?.length ?? 0)}
                <p class="check"><Check size={16} />{t('run.verifyOk', { n: fmtNumber(lang(), r.files) })} ({fmtSize(lang(), r.bytes)})</p>
              {:else}
                <p class="flex items-center gap-2 font-semibold text-bad"><CircleAlert size={16} />{t('run.verifyBad', { n: r.issues?.length ?? 0 })}</p>
              {/if}
            {/if}
          </li>
        {/each}
      </ul>
      {#if allIssues.length}
        <div class="mt-4 rounded-xl bg-warn-bg p-3 text-warn">
          <button class="flex w-full items-center gap-2 text-left font-semibold" onclick={() => (showIssues = !showIssues)}>
            <TriangleAlert size={16} />
            <span class="flex-1">{job.kind === 'backup' ? t('run.ok.skipped', { n: allIssues.length }) : t('run.problems', { n: allIssues.length })}</span>
            <span class="text-[12px] underline">{t('run.showIssues')}</span>
          </button>
          {#if showIssues}
            <ul class="selectable mt-2 max-h-40 overflow-auto text-[12px] scroll-thin">
              {#each allIssues as i, n (n)}
                <li class="py-0.5"><span class="font-mono">{i.path}</span> — {issueText(i.code, i.message)}</li>
              {/each}
            </ul>
          {/if}
        </div>
      {/if}
    {/if}
  </div>

  <div class="mt-7 flex justify-end gap-2">
    {#if job.phase === 'awaiting'}
      <button class="btn btn-ghost" disabled={answeredPlan === job.plan} onclick={() => answer(false)}>{t('common.cancel')}</button>
      <button class="btn btn-primary" disabled={answeredPlan === job.plan} onclick={() => answer(true)}>
        {job.plan?.massChange ? t('run.mass.confirm') : t('run.start')}
      </button>
    {:else if !finished}
      {#if job.kind === 'backup' && (job.phase === 'running' || job.phase === 'scanning')}
        {#if job.paused}
          <button class="btn btn-secondary" onclick={() => api.Resume()}><Play size={16} />{t('run.resume')}</button>
        {:else}
          <button class="btn btn-secondary" onclick={() => api.Pause()}><Pause size={16} />{t('run.pause')}</button>
        {/if}
      {/if}
      <button class="btn btn-ghost" onclick={() => api.Cancel()}>{t('common.cancel')}</button>
    {:else}
      {#if job.kind === 'restore' && job.dest && job.phase === 'done'}
        <button class="btn btn-secondary" onclick={() => api.OpenFolder(job.dest!)}>{t('common.openFolder')}</button>
      {:else if job.kind === 'backup' && store.state?.vault?.path}
        <button class="btn btn-secondary" onclick={() => api.OpenFolder(store.state!.vault!.path)}>{t('run.openBackup')}</button>
      {/if}
      <button class="btn btn-primary" onclick={close}>{t('common.done')}</button>
    {/if}
  </div>
</Sheet>

<style>
  .check {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 2px 0;
    animation: fade-up 280ms var(--ease-spring) both;
  }
  .check :global(svg) {
    color: var(--ok);
    flex: none;
  }
  li:nth-child(1) .check:nth-of-type(2) {
    animation-delay: 80ms;
  }
  li:nth-child(1) .check:nth-of-type(3) {
    animation-delay: 160ms;
  }
  li:nth-child(1) .check:nth-of-type(4) {
    animation-delay: 240ms;
  }
</style>
