<script lang="ts">
  import { Sun, Moon, Monitor, Trash2, HardDrive, ExternalLink, Plus } from '@lucide/svelte';
  import Mascot from '../lib/ui/Mascot.svelte';
  import Sheet from '../lib/ui/Sheet.svelte';
  import AddSource from '../lib/AddSource.svelte';
  import DiskPicker from '../lib/DiskPicker.svelte';
  import { api, errText, lang, store, t, toast } from '../lib/store.svelte';
  import { fmtSize } from '../lib/i18n';

  const st = $derived(store.state!);
  let excludes = $state((store.state?.exclude ?? []).join('\n'));
  let adding = $state(false);
  let picking = $state(false);

  async function saveExcludes() {
    await api.SetExcludes(excludes.split('\n'));
    toast(t('set.saved'));
  }

  async function check() {
    try {
      await api.CheckBackups();
    } catch (e) {
      toast(errText(e));
    }
  }
</script>

<div class="mx-auto flex max-w-[720px] flex-col gap-6 px-8 py-8">
  <h1 class="font-display text-[26px]">{t('set.title')}</h1>

  <section class="card divide-y divide-border">
    <h2 class="px-5 py-3 text-[13px] font-semibold text-muted">{t('set.general')}</h2>
    <div class="flex items-center gap-3 px-5 py-3">
      <span class="flex-1">{t('set.language')}</span>
      <div class="seg" role="radiogroup" aria-label={t('set.language')}>
        {#each [['system', 'Auto'], ['tr', 'Türkçe'], ['en', 'English']] as [v, label] (v)}
          <button role="radio" aria-checked={st.language === v} class:on={st.language === v} onclick={() => api.SetLanguage(v)}>{label}</button>
        {/each}
      </div>
    </div>
    <div class="flex items-center gap-3 px-5 py-3">
      <span class="flex-1">{t('set.theme')}</span>
      <div class="seg" role="radiogroup" aria-label={t('set.theme')}>
        <button role="radio" aria-checked={st.theme === 'system'} class:on={st.theme === 'system'} onclick={() => api.SetTheme('system')}><Monitor size={14} />{t('set.theme.system')}</button>
        <button role="radio" aria-checked={st.theme === 'light'} class:on={st.theme === 'light'} onclick={() => api.SetTheme('light')}><Sun size={14} />{t('set.theme.light')}</button>
        <button role="radio" aria-checked={st.theme === 'dark'} class:on={st.theme === 'dark'} onclick={() => api.SetTheme('dark')}><Moon size={14} />{t('set.theme.dark')}</button>
      </div>
    </div>
    <label class="flex items-center gap-3 px-5 py-3">
      <span class="flex-1">
        <span class="block">{t('set.confirm')}</span>
        <span class="block text-[12px] text-muted">{t('set.confirm.hint')}</span>
      </span>
      <input type="checkbox" class="switch" checked={st.confirmBeforeRun} onchange={(e) => api.SetConfirmBeforeRun(e.currentTarget.checked)} />
    </label>
  </section>

  <section class="card divide-y divide-border">
    <div class="flex items-center px-5 py-2">
      <h2 class="flex-1 text-[13px] font-semibold text-muted">{t('set.locations')}</h2>
      <button class="btn btn-ghost h-8" onclick={() => (adding = true)}><Plus size={16} />{t('common.add')}</button>
    </div>
    {#each st.sources as s (s.id)}
      <div class="flex items-center gap-3 px-5 py-3">
        <span class="min-w-0 flex-1">
          <span class="block font-semibold">{s.name}</span>
          <span class="block truncate font-mono text-[12px] text-muted">{s.path}</span>
        </span>
        <button class="btn btn-ghost" aria-label={t('common.remove')} onclick={() => api.RemoveSource(s.id)}><Trash2 size={16} /></button>
      </div>
    {/each}
  </section>

  <section class="card divide-y divide-border">
    <h2 class="px-5 py-3 text-[13px] font-semibold text-muted">{t('set.disk')}</h2>
    <div class="flex items-center gap-3 px-5 py-3">
      <HardDrive size={18} class="text-accent" />
      <span class="min-w-0 flex-1">
        <span class="block font-semibold">{st.vault?.label || '—'}</span>
        <span class="block truncate font-mono text-[12px] text-muted">{st.vault?.path ?? ''}</span>
      </span>
      {#if st.vault?.connected}<span class="num text-[13px] text-muted">{t('common.free', { size: fmtSize(lang(), st.vault.free) })}</span>{/if}
      <button class="btn btn-secondary" onclick={() => (picking = true)}>{t('common.change')}</button>
    </div>
    <div class="flex items-center gap-3 px-5 py-3">
      <span class="flex-1">
        <span class="block">{t('set.check')}</span>
        <span class="block text-[12px] text-muted">{t('set.check.hint')}</span>
      </span>
      <button class="btn btn-secondary" disabled={!st.vault?.connected || !!store.job} onclick={check}>{t('set.check')}</button>
    </div>
  </section>

  <section class="card px-5 py-4">
    <h2 class="text-[13px] font-semibold text-muted">{t('set.exclude')}</h2>
    <p class="mt-1 text-[12px] text-muted">{t('set.exclude.hint')}</p>
    <textarea class="input mt-3 h-24 py-2 font-mono text-[13px]" bind:value={excludes} spellcheck="false"></textarea>
    <p class="mt-2 text-[12px] text-muted">{t('set.exclude.defaults')}: <span class="font-mono">{st.defaultExclude.join('  ')}</span></p>
    <div class="mt-3 flex justify-end"><button class="btn btn-secondary" onclick={saveExcludes}>{t('set.save')}</button></div>
  </section>

  <section class="card flex items-center gap-4 px-5 py-4">
    <Mascot size={56} />
    <div class="flex-1">
      <p class="font-semibold">bleen <span class="num font-normal text-muted">{st.version}</span></p>
      <p class="text-[13px] text-muted">{t('set.about.body')}</p>
      <button class="mt-1 flex items-center gap-1 text-[13px] text-accent" onclick={() => api.OpenURL('https://github.com/kaanbahasever/bleen')}>
        {t('set.about.license')} <ExternalLink size={12} />
      </button>
    </div>
  </section>
</div>

{#if adding}
  <Sheet onclose={() => (adding = false)}>
    <h2 class="mb-4 font-display text-[22px]">{t('add.title')}</h2>
    <AddSource onadded={() => (adding = false)} />
  </Sheet>
{/if}
{#if picking}
  <Sheet onclose={() => (picking = false)}>
    <h2 class="mb-4 font-display text-[22px]">{t('onb.vault.title')}</h2>
    <DiskPicker onchosen={() => (picking = false)} />
  </Sheet>
{/if}

<style>
  .seg {
    display: inline-flex;
    padding: 3px;
    border-radius: 10px;
    background: var(--surface-2);
  }
  .seg button {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 28px;
    padding: 0 10px;
    border-radius: 8px;
    border: 0;
    background: none;
    color: var(--muted);
    font: inherit;
    font-size: 13px;
    cursor: pointer;
  }
  .seg button.on {
    background: var(--surface);
    color: var(--text);
    box-shadow: var(--shadow-sm);
    font-weight: 600;
  }
  button:not(.btn):not(.seg button) {
    background: none;
    border: 0;
    font: inherit;
    cursor: pointer;
  }
  .switch {
    appearance: none;
    width: 38px;
    height: 22px;
    border-radius: 99px;
    background: var(--border);
    position: relative;
    cursor: pointer;
    transition: background 200ms;
  }
  .switch::after {
    content: '';
    position: absolute;
    top: 3px;
    left: 3px;
    width: 16px;
    height: 16px;
    border-radius: 99px;
    background: #fff;
    transition: transform 200ms var(--ease-spring);
  }
  .switch:checked {
    background: var(--accent);
  }
  .switch:checked::after {
    transform: translateX(16px);
  }
</style>
