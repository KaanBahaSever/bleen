<script lang="ts">
  import { House, History as HistoryIcon, Activity as ActivityIcon, Settings as SettingsIcon, HardDrive } from '@lucide/svelte';
  import { onMount } from 'svelte';
  import Mascot from './lib/ui/Mascot.svelte';
  import RunSheet from './lib/RunSheet.svelte';
  import Onboarding from './pages/Onboarding.svelte';
  import Home from './pages/Home.svelte';
  import History from './pages/History.svelte';
  import Activity from './pages/Activity.svelte';
  import Settings from './pages/Settings.svelte';
  import { backupNow, init, store, t, type Route } from './lib/store.svelte';

  let ready = $state(false);
  onMount(async () => {
    await init();
    ready = true;
  });

  const nav: { route: Route; key: 'nav.home' | 'nav.history' | 'nav.activity' | 'nav.settings'; icon: typeof House }[] = [
    { route: 'home', key: 'nav.home', icon: House },
    { route: 'history', key: 'nav.history', icon: HistoryIcon },
    { route: 'activity', key: 'nav.activity', icon: ActivityIcon },
    { route: 'settings', key: 'nav.settings', icon: SettingsIcon },
  ];

  function keys(e: KeyboardEvent) {
    if (!(e.ctrlKey || e.metaKey) || store.job) return;
    if (e.key === 'b' && store.state?.vault?.connected && store.state.sources.length) {
      e.preventDefault();
      backupNow();
    } else if (e.key === ',') {
      e.preventDefault();
      store.route = 'settings';
    }
  }
</script>

<svelte:window onkeydown={keys} />

{#if ready && store.state}
  {#if !store.onboardingDone}
    <Onboarding />
  {:else}
    <div class="flex h-full">
      <aside class="flex w-[208px] flex-none flex-col border-r border-border px-3 py-4">
        <div class="mb-6 flex items-center gap-2 px-2">
          <Mascot size={30} />
          <span class="font-display text-[22px] leading-none">bleen</span>
        </div>
        <nav class="flex flex-col gap-0.5">
          {#each nav as n (n.route)}
            <button class="nav" class:on={store.route === n.route} aria-current={store.route === n.route ? 'page' : undefined} onclick={() => (store.route = n.route)}>
              <n.icon size={17} />{t(n.key)}
            </button>
          {/each}
        </nav>
        <div class="mt-auto px-2 text-[12px] text-muted">
          {#if store.state.vault}
            <div class="flex items-center gap-2">
              <span class="h-2 w-2 rounded-full {store.state.vault.connected ? 'bg-ok' : 'bg-warn'}"></span>
              <HardDrive size={13} />
              <span class="truncate">{store.state.vault.label || store.state.vault.path}</span>
            </div>
          {/if}
        </div>
      </aside>
      <main class="scroll-thin min-w-0 flex-1 overflow-auto">
        {#key store.route}
          <div class="fade-up h-full">
            {#if store.route === 'home'}<Home />
            {:else if store.route === 'history'}<History />
            {:else if store.route === 'activity'}<Activity />
            {:else}<Settings />{/if}
          </div>
        {/key}
      </main>
    </div>
  {/if}

  {#if store.job}
    <RunSheet />
  {/if}

  {#if store.toast}
    <div class="toast card fade-up" role="status">{store.toast}</div>
  {/if}
{/if}

<style>
  .nav {
    display: flex;
    align-items: center;
    gap: 10px;
    height: 36px;
    padding: 0 10px;
    border-radius: 10px;
    background: none;
    border: 0;
    color: var(--muted);
    font: inherit;
    font-weight: 500;
    cursor: pointer;
    text-align: left;
  }
  .nav:hover {
    background: var(--surface-2);
    color: var(--text);
  }
  .nav.on {
    background: var(--accent-soft);
    color: var(--accent);
    font-weight: 600;
  }
  .toast {
    position: fixed;
    bottom: 20px;
    left: 50%;
    translate: -50% 0;
    padding: 10px 16px;
    z-index: 60;
    box-shadow: var(--shadow-md);
  }
</style>
