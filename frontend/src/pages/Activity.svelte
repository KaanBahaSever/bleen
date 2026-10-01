<script lang="ts">
  import { Check, CircleAlert, X, Minus, TriangleAlert, FileDown } from '@lucide/svelte';
  import Mascot from '../lib/ui/Mascot.svelte';
  import { api, issueText, errText, lang, store, t, toast } from '../lib/store.svelte';
  import { fmtDate, fmtNumber, fmtSize } from '../lib/i18n';
  import type { app } from '../lib/wailsjs/go/models';

  let runs = $state<app.RunRecord[]>([]);
  let open = $state('');

  $effect(() => {
    void store.job?.phase;
    api.Activity().then((r) => (runs = r ?? []));
  });

  async function exportReport() {
    try {
      const p = await api.ExportReport(t('act.export'));
      if (p) toast(t('act.exported'));
    } catch (e) {
      toast(errText(e));
    }
  }

  function title(r: app.RunRecord) {
    const kind = r.kind === 'restore' ? t('act.restore') : r.kind === 'verify' ? t('act.verify') : t('act.backup');
    return r.source ? `${kind} · ${r.source}` : kind;
  }

  function summary(r: app.RunRecord) {
    if (r.result === 'failed') return errText(r.error?.code);
    if (r.result === 'cancelled') return t('act.cancelled');
    if (r.result === 'nothing') return t('act.nothing');
    if (r.kind === 'verify') return r.issues?.length ? t('act.verifyBad', { n: r.issues.length }) : t('run.verifyOk', { n: r.files });
    if (r.kind === 'restore') return `${t('run.restored', { n: fmtNumber(lang(), r.files) })} · ${fmtSize(lang(), r.bytes)}`;
    return `${t('run.ok.files', { n: fmtNumber(lang(), r.files) })} · ${fmtSize(lang(), r.stored)}`;
  }
</script>

<div class="mx-auto max-w-[880px] px-8 py-8">
  <div class="mb-5 flex items-center">
    <h1 class="flex-1 font-display text-[26px]">{t('act.title')}</h1>
    {#if runs.length}
      <button class="btn btn-secondary" onclick={exportReport}><FileDown size={16} />{t('act.export')}</button>
    {/if}
  </div>
  {#if !runs.length}
    <div class="card flex flex-col items-center px-6 py-12 text-center">
      <Mascot mood="sleeping" size={88} />
      <p class="mt-4 text-muted">{t('act.empty')}</p>
    </div>
  {:else}
    <ul class="card divide-y divide-border">
      {#each runs as r (r.id)}
        {@const issues = r.issues ?? []}
        <li>
          <button class="flex w-full items-center gap-3 px-5 py-3 text-left" onclick={() => (open = open === r.id ? '' : r.id)}>
            <span class="icon {r.result === 'failed' ? 'bad' : issues.length ? 'warn' : r.result === 'cancelled' || r.result === 'nothing' ? 'muted' : 'ok'}">
              {#if r.result === 'failed'}<CircleAlert size={16} />
              {:else if r.result === 'cancelled'}<X size={16} />
              {:else if r.result === 'nothing'}<Minus size={16} />
              {:else if issues.length}<TriangleAlert size={16} />
              {:else}<Check size={16} />{/if}
            </span>
            <span class="min-w-0 flex-1">
              <span class="block truncate font-semibold">{title(r)}</span>
              <span class="num block truncate text-[13px] text-muted">{summary(r)}</span>
            </span>
            <span class="num text-[12px] text-muted">{fmtDate(lang(), r.startedAt)}</span>
          </button>
          {#if open === r.id && (issues.length || r.archives?.length || r.dest || r.error)}
            <div class="selectable px-5 pb-4 pl-14 text-[12px]">
              {#if r.error}<p class="text-bad">{r.error.message}</p>{/if}
              {#each r.archives ?? [] as a (a)}<p class="font-mono text-muted">→ {a}</p>{/each}
              {#if r.dest}<p class="font-mono text-muted">→ {r.dest}</p>{/if}
              {#each issues as i, n (n)}
                <p class="mt-1"><span class="font-mono">{i.path}</span> <span class="text-warn">— {issueText(i.code, i.message)}</span></p>
              {/each}
            </div>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  button {
    background: none;
    border: 0;
    color: inherit;
    font: inherit;
    cursor: pointer;
  }
  .icon {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    border-radius: 99px;
    flex: none;
  }
  .ok {
    background: var(--ok-bg);
    color: var(--ok);
  }
  .warn {
    background: var(--warn-bg);
    color: var(--warn);
  }
  .bad {
    background: var(--bad-bg);
    color: var(--bad);
  }
  .muted {
    background: var(--surface-2);
    color: var(--muted);
  }
</style>
