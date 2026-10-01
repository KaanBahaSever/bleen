// Single source of truth on the frontend. The Go side owns the real state;
// this mirrors it from GetState() and the "state" / "job" events.
import * as api from './wailsjs/go/ui/Bridge';
import { EventsOn } from './wailsjs/runtime/runtime';
import type { app } from './wailsjs/go/models';
import { translate, resolveLang, hasKey, type Key, type Lang } from './i18n';

export type Route = 'home' | 'history' | 'activity' | 'settings';

export const store = $state({
  state: null as app.State | null,
  job: null as app.JobState | null,
  route: 'home' as Route,
  historySource: '' as string, // catalog source id preselected when opening History
  onboardingDone: false,
  toast: '' as string,
});

export function t(key: Key, vars?: Record<string, string | number>): string {
  return translate(lang(), key, vars);
}

export function lang(): Lang {
  return resolveLang(store.state?.language ?? 'system');
}

/** Maps an error (Go error string or code) to friendly text. */
export function errText(err: unknown): string {
  const raw = String((err as Error)?.message ?? err ?? '');
  const code = raw.match(/E_[A-Z_]+/)?.[0] ?? '';
  const key = `err.${code}`;
  return hasKey(key) ? t(key) : raw || t('err.E_UNKNOWN');
}

export function issueText(code: string, message: string): string {
  const key = `err.${code}`;
  return hasKey(key) ? t(key) : message;
}

let toastTimer: ReturnType<typeof setTimeout> | undefined;
export function toast(msg: string) {
  store.toast = msg;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => (store.toast = ''), 3500);
}

function applyTheme(theme: string) {
  const dark =
    theme === 'dark' || (theme !== 'light' && window.matchMedia('(prefers-color-scheme: dark)').matches);
  document.documentElement.dataset.theme = dark ? 'dark' : 'light';
  document.documentElement.lang = lang();
}

export async function init() {
  EventsOn('state', (s: app.State) => {
    store.state = s;
    store.job = s.job ?? null;
    applyTheme(s.theme);
  });
  EventsOn('job', (j: app.JobState) => {
    store.job = j;
  });
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () =>
    applyTheme(store.state?.theme ?? 'system'),
  );
  const s = await api.GetState();
  store.state = s;
  store.job = s.job ?? null;
  store.onboardingDone = (s.sources ?? []).length > 0 && !!s.vault;
  const route = new URLSearchParams(location.search).get('route');
  if (import.meta.env.DEV && route) store.route = route as Route;
  applyTheme(s.theme);
  if (s.notice === 'E_CONFIG_RESET') {
    toast(t('notice.configReset'));
    api.DismissNotice();
  }
}

export async function backupNow(ids: string[] = [], full = false) {
  try {
    await api.BackupNow(ids, full);
  } catch (e) {
    toast(errText(e));
  }
}

export { api };
