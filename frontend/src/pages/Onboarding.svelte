<script lang="ts">
  import { Check, Folder } from '@lucide/svelte';
  import Mascot from '../lib/ui/Mascot.svelte';
  import AddSource from '../lib/AddSource.svelte';
  import DiskPicker from '../lib/DiskPicker.svelte';
  import { api, backupNow, store, t } from '../lib/store.svelte';

  let step = $state(0);
  const sources = $derived(store.state?.sources ?? []);

  function finish(start: boolean) {
    store.onboardingDone = true;
    store.route = 'home';
    if (start) backupNow();
  }
</script>

<div class="h-full overflow-auto scroll-thin">
  <div class="mx-auto flex min-h-full max-w-[560px] flex-col justify-center px-6 py-10">
    <div class="mb-6 flex gap-1.5" aria-hidden="true">
      {#each [0, 1, 2, 3] as i (i)}
        <span class="h-1.5 flex-1 rounded-full {i <= step ? 'bg-accent' : 'bg-surface-2'}"></span>
      {/each}
    </div>

    {#key step}
      <div class="fade-up">
        {#if step === 0}
          <Mascot size={112} />
          <h1 class="mt-5 font-display text-[32px] leading-10">{t('onb.welcome.title')}</h1>
          <p class="mt-3 text-[16px] leading-6 text-muted">{t('onb.welcome.body')}</p>
          <p class="mt-4 text-[13px] text-muted">{t('onb.welcome.promise')}</p>
          <div class="mt-6 flex items-center gap-3">
            <button class="btn btn-primary btn-lg" onclick={() => (step = 1)}>{t('onb.welcome.cta')}</button>
            <div class="ml-auto flex gap-1" role="group" aria-label={t('set.language')}>
              {#each [['tr', 'Türkçe'], ['en', 'English']] as [code, name] (code)}
                <button
                  class="btn {store.state?.language === code ? 'btn-secondary' : 'btn-ghost'}"
                  onclick={() => api.SetLanguage(code)}>{name}</button
                >
              {/each}
            </div>
          </div>
        {:else if step === 1}
          <h1 class="font-display text-[28px] leading-9">{t('onb.source.title')}</h1>
          <p class="mt-2 text-muted">{t('onb.source.body')}</p>
          <div class="mt-5"><AddSource /></div>
          {#if sources.length}
            <ul class="mt-5 flex flex-col gap-2">
              {#each sources as s (s.id)}
                <li class="card flex items-center gap-3 px-4 py-3">
                  <Folder size={18} class="text-accent" />
                  <span class="flex-1 truncate">
                    <span class="font-semibold">{s.name}</span>
                    <span class="ml-2 font-mono text-[12px] text-muted">{s.path}</span>
                  </span>
                  <Check size={16} class="text-ok" />
                </li>
              {/each}
            </ul>
          {/if}
          <div class="mt-6 flex gap-2">
            <button class="btn btn-ghost" onclick={() => (step = 0)}>{t('common.back')}</button>
            <button class="btn btn-primary ml-auto" disabled={!sources.length} onclick={() => (step = 2)}>
              {t('common.next')}
            </button>
          </div>
        {:else if step === 2}
          <h1 class="font-display text-[28px] leading-9">{t('onb.vault.title')}</h1>
          <p class="mt-2 text-muted">{t('onb.vault.body')}</p>
          <div class="mt-5"><DiskPicker onchosen={() => (step = 3)} /></div>
          <div class="mt-6 flex gap-2">
            <button class="btn btn-ghost" onclick={() => (step = 1)}>{t('common.back')}</button>
            {#if store.state?.vault}
              <button class="btn btn-primary ml-auto" onclick={() => (step = 3)}>{t('common.next')}</button>
            {/if}
          </div>
        {:else}
          <Mascot mood="happy" size={112} />
          <h1 class="mt-5 font-display text-[32px] leading-10">{t('onb.summary.title')}</h1>
          <p class="mt-3 text-[16px] leading-6 text-muted">{t('onb.summary.body')}</p>
          <div class="card mt-5 px-4 py-3 text-[13px]">
            {#each sources as s (s.id)}
              <div class="truncate"><span class="font-semibold">{s.name}</span> <span class="font-mono text-muted">{s.path}</span></div>
            {/each}
            <div class="mt-2 truncate text-muted">→ <span class="font-mono">{store.state?.vault?.path}</span></div>
          </div>
          <div class="mt-6 flex gap-2">
            <button class="btn btn-primary btn-lg" onclick={() => finish(true)}>{t('onb.summary.start')}</button>
            <button class="btn btn-ghost btn-lg" onclick={() => finish(false)}>{t('onb.summary.later')}</button>
          </div>
        {/if}
      </div>
    {/key}
  </div>
</div>
