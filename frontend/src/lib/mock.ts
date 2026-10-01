// Development-only fake backend: lets the UI run in a plain browser (no Go,
// no native window) for design work, screenshots and UI tests.
// Loaded by main.ts only when import.meta.env.DEV and window.go is missing.
// Scenes: ?scene=onboarding | home (default) | locked | running | history

type Cb = (data: any) => void;
const listeners = new Map<string, Set<Cb>>();
const emit = (name: string, data: any) => listeners.get(name)?.forEach((cb) => cb(structuredClone(data)));

const params = new URLSearchParams(location.search);
const scene = params.get('scene') ?? 'home';
const freeze = params.has('freeze'); // stop the fake backup at 62% for screenshots
if (params.has('still')) {
  // Screenshots: headless browsers don't advance CSS animations under virtual time.
  const st = document.createElement('style');
  st.textContent = '*,*::before,*::after{animation:none!important;transition:none!important}';
  document.head.appendChild(st);
}
const lang = new URLSearchParams(location.search).get('lang') ?? 'tr';
// All fake times hang off one anchor: the latest 15:30 that is at least two
// hours ago. "Today" entries stay before it, so nothing is ever in the
// future and every file is older than the backup that contains it.
const anchor = (() => {
  const d = new Date();
  d.setHours(15, 30, 0, 0);
  if (d.getTime() > Date.now() - 2 * 3_600_000) d.setDate(d.getDate() - 1);
  return d;
})();
const day = (n: number, h = 18, m = 0) => {
  const d = new Date(anchor);
  d.setDate(d.getDate() - n);
  d.setHours(h, m, 0, 0);
  return d.toISOString();
};

const backups = [
  { id: 'b1', kind: 'full', generation: 1, finishedAt: day(6, 18, 2), new: 10003, modified: 0, deleted: 0, skipped: 0, stored: 12.4e9, sourceSize: 14.1e9 },
  { id: 'b2', kind: 'incremental', generation: 1, finishedAt: day(5, 18, 1), new: 52, modified: 295, deleted: 4, skipped: 0, stored: 418e6, sourceSize: 460e6 },
  { id: 'b3', kind: 'incremental', generation: 1, finishedAt: day(4, 17, 55), new: 7, modified: 23, deleted: 2, skipped: 0, stored: 185e6, sourceSize: 190e6 },
  { id: 'b4', kind: 'incremental', generation: 1, finishedAt: day(2, 18, 10), new: 18, modified: 61, deleted: 0, skipped: 3, stored: 320e6, sourceSize: 330e6 },
  { id: 'b5', kind: 'incremental', generation: 1, finishedAt: day(0, 14, 30), new: 4, modified: 12, deleted: 1, skipped: 0, stored: 96e6, sourceSize: 99e6 },
];

const state: any = {
  version: 'v0.3.0-beta.1',
  language: lang,
  theme: new URLSearchParams(location.search).get('theme') ?? 'light',
  confirmBeforeRun: true,
  exclude: [],
  defaultExclude: ['~$*', 'Thumbs.db', 'desktop.ini', '.DS_Store', '*.tmp', '$RECYCLE.BIN/', 'System Volume Information/'],
  os: 'windows',
  keepGenerations: 3,
  newFullEvery: 30,
  automation: { supported: true, enabled: false, time: '18:00' },
  vault: { id: 'v1', label: 'Mavi SanDisk', path: 'E:\\bleen', connected: true, free: 412e9, fsType: 'exFAT', encrypted: scene === 'locked', locked: scene === 'locked' },
  vaults: [
    { id: 'v1', label: 'Mavi SanDisk', path: 'E:\\bleen', connected: true, active: true },
    { id: 'v2', label: 'Ofis diski', path: 'F:\\bleen', connected: false, active: false },
  ],
  sources: [
    { id: 's1', name: '_proje', path: '\\\\SERVER\\Root\\_proje', reachable: true, vaultSourceId: 'c1', lastBackup: day(0, 14, 30), backups: 5, lastIssues: 0, recent: backups.slice(1).map((b) => b.stored) },
    { id: 's2', name: 'muhasebe', path: '\\\\SERVER\\Root\\muhasebe', reachable: true, vaultSourceId: 'c2', lastBackup: day(2, 18, 10), backups: 3, lastIssues: 3, recent: [210e6, 90e6, 140e6] },
    { id: 's3', name: 'Tasarımlar', path: 'D:\\Tasarımlar', reachable: true, vaultSourceId: '', lastBackup: '0001-01-01T00:00:00Z', backups: 0, lastIssues: 0, recent: [] },
  ],
  job: null,
};
if (scene === 'onboarding') {
  state.sources = [];
  state.vault = null;
  state.vaults = [];
}

const runs: any[] = [
  { id: 'r1', kind: 'backup', source: '_proje', startedAt: day(0, 14, 29), finishedAt: day(0, 14, 30), result: 'done', backupKind: 'incremental', files: 16, deduped: 2, bytes: 99e6, stored: 96e6, verified: true },
  { id: 'r2', kind: 'restore', source: '_proje', startedAt: day(1, 10, 2), finishedAt: day(1, 10, 3), result: 'done', files: 214, bytes: 1.2e9, dest: 'D:\\Geri yükleme\\_proje', verified: true },
  { id: 'r3', kind: 'backup', source: 'muhasebe', startedAt: day(2, 18, 9), finishedAt: day(2, 18, 10), result: 'done', backupKind: 'incremental', files: 79, deduped: 0, bytes: 330e6, stored: 320e6, verified: true,
    issues: [{ path: 'Mizan 2026.xlsx', code: 'E_FILE_LOCKED', message: 'in use' }, { path: 'Fatura/Eylül.pdf', code: 'E_FILE_LOCKED', message: 'in use' }, { path: 'bordro.xlsx', code: 'E_FILE_UNSTABLE', message: 'changing' }] },
  { id: 'r4', kind: 'verify', source: '', startedAt: day(3, 9, 0), finishedAt: day(3, 9, 4), result: 'done', files: 9, bytes: 13.4e9, verified: true },
];

const tree: Record<string, any[]> = {
  '': [
    { name: 'Çizimler', path: 'Çizimler', dir: true, size: 8.2e9 },
    { name: 'Muhasebe 2026', path: 'Muhasebe 2026', dir: true, size: 1.1e9 },
    { name: 'Teklifler', path: 'Teklifler', dir: true, size: 3.8e9 },
    { name: 'proje planı.xlsx', path: 'proje planı.xlsx', dir: false, size: 48213, modTime: day(0, 11, 12) },
    { name: 'toplantı notları.docx', path: 'toplantı notları.docx', dir: false, size: 24100, modTime: day(1, 16, 40) },
  ],
};

function emitState() {
  emit('state', state);
}

async function runJob(kind: string, name: string) {
  const j: any = { id: 'j1', kind, phase: 'scanning', sourceName: name, index: 1, count: 1, scanned: 0, plan: null, progress: { filesDone: 0, filesTotal: 0, bytesDone: 0, bytesTotal: 0, current: '' }, results: [], error: null, paused: false };
  state.job = j;
  const tick = (ms: number) => new Promise((r) => setTimeout(r, ms));
  for (let n = 0; n <= 12408; n += 1551) {
    j.scanned = n;
    emit('job', j);
    await tick(80);
  }
  j.phase = 'awaiting';
  j.plan = { sourceName: name, origin: '\\\\SERVER\\Root\\_proje', kind: 'incremental', lastFull: backups[0].finishedAt, lastBackup: backups[4].finishedAt, totalFiles: 10044, new: 52, modified: 295, deleted: 4, bytesToRead: 460e6, estStored: 418e6, vaultFree: 412e9, massChange: false, scanIssues: 0, nothingToDo: false, changedRatio: 0.03 };
  emit('job', j);
}

const files = ['Çizimler/cephe-A.dwg', 'Teklifler/2026-09 Teklif.docx', 'Muhasebe 2026/Mizan.xlsx', 'proje planı.xlsx'];
async function progress() {
  const j = state.job;
  j.phase = 'running';
  const total = 347;
  for (let i = 0; i <= total; i += 7) {
    if (!state.job) return;
    while (j.paused) await new Promise((r) => setTimeout(r, 100));
    if (freeze && i >= total * 0.62) return;
    j.progress = { filesDone: i, filesTotal: total, bytesDone: (i / total) * 460e6, bytesTotal: 460e6, current: files[i % files.length] };
    emit('job', j);
    await new Promise((r) => setTimeout(r, 60));
  }
  j.phase = 'verifying';
  emit('job', j);
  await new Promise((r) => setTimeout(r, 700));
  j.phase = 'done';
  j.results = [{ id: 'x', kind: 'backup', source: j.sourceName, result: 'done', files: 347, deduped: 5, stored: 418e6, bytes: 460e6, verified: true, pruned: 0 }];
  emit('job', j);
}

const Bridge: Record<string, (...a: any[]) => Promise<any>> = {
  GetState: async () => structuredClone(state),
  SetLanguage: async (l) => ((state.language = l), emitState()),
  SetTheme: async (t) => ((state.theme = t), emitState()),
  SetConfirmBeforeRun: async (v) => ((state.confirmBeforeRun = v), emitState()),
  SetExcludes: async (e) => ((state.exclude = e), emitState()),
  SetRetention: async (k, n) => ((state.keepGenerations = k), (state.newFullEvery = n), emitState()),
  SetAutomation: async (on, at) => ((state.automation = { ...state.automation, enabled: on, time: at }), emitState()),
  AddSource: async (p) => {
    const s = { id: 's' + Date.now(), name: p.split(/[\\/]/).pop(), path: p, reachable: true, vaultSourceId: '', lastBackup: '0001-01-01T00:00:00Z', backups: 0, lastIssues: 0, recent: [] };
    state.sources.push(s);
    emitState();
    return s;
  },
  AddSourceAs: async (p) => Bridge.AddSource(p),
  BreakLock: async () => {},
  DismissNotice: async () => {},
  RemoveSource: async (id) => ((state.sources = state.sources.filter((s: any) => s.id !== id)), emitState()),
  RenameSource: async () => {},
  Drives: async () => [
    { path: 'E:\\', label: 'Mavi SanDisk', fsType: 'exFAT', removable: true, free: 412e9, total: 1000e9 },
    { path: 'F:\\', label: 'Eski USB', fsType: 'FAT32', removable: true, free: 21e9, total: 32e9 },
  ],
  UseVaultFolder: async (dir, pw) => {
    state.vault = { id: 'v1', label: 'Mavi SanDisk', path: dir + 'bleen', connected: true, free: 412e9, fsType: 'exFAT', encrypted: !!pw, locked: false };
    state.vaults = [{ id: 'v1', label: 'Mavi SanDisk', path: dir + 'bleen', connected: true, active: true }];
    emitState();
    return state.vault;
  },
  SwitchVault: async () => {},
  ForgetVault: async () => {},
  Unlock: async (pw) => {
    if (pw !== 'parola123') throw 'E_WRONG_PASSWORD';
    state.vault.locked = false;
    emitState();
  },
  Lock: async () => ((state.vault.locked = true), emitState()),
  BackupNow: async () => (runJob('backup', '_proje'), 'j1'),
  ConfirmPlan: async (ok) => (ok ? progress() : ((state.job.phase = 'cancelled'), emit('job', state.job))),
  Pause: async () => ((state.job.paused = true), emit('job', state.job)),
  Resume: async () => ((state.job.paused = false), emit('job', state.job)),
  Cancel: async () => ((state.job.phase = 'cancelled'), emit('job', state.job)),
  DismissJob: async () => ((state.job = null), emitState()),
  ListBackups: async () => [
    { id: 'c1', name: '_proje', folder: '_proje', origin: '\\\\SERVER\\Root\\_proje', host: 'OFIS-PC', backups },
    { id: 'c2', name: 'muhasebe', folder: 'muhasebe', origin: '\\\\SERVER\\Root\\muhasebe', host: 'OFIS-PC', backups: [backups[0], backups[1], backups[3]] },
  ],
  Browse: async (_s, _b, dir) => ({
    entries: tree[dir] ?? [
      { name: 'Eylül', path: dir + '/Eylül', dir: true, size: 2.1e8 },
      { name: 'rapor.xlsx', path: dir + '/rapor.xlsx', dir: false, size: 91234, modTime: day(3, 9, 30) },
    ],
    totalFiles: 10044,
    totalBytes: 14.1e9,
  }),
  Restore: async () => 'j2',
  Export: async () => 'j3',
  SuggestExportFile: async (name) => `C:\\Users\\Ayşe\\Desktop\\${name} (geri yüklenen).zip`,
  ChooseZipFile: async () => '',
  SuggestRestoreFolder: async (name) => `C:\\Users\\Ayşe\\Desktop\\${name} (geri yüklenen)`,
  CheckBackups: async () => 'j3',
  Activity: async () => runs,
  ExportReport: async () => 'C:\\Users\\Ayşe\\Desktop\\bleen-report.html',
  OpenFolder: async () => {},
  OpenURL: async () => {},
  ChooseFolder: async () => '',
};

(window as any).go = { ui: { Bridge } };
(window as any).runtime = {
  EventsOnMultiple: (name: string, cb: Cb) => {
    if (!listeners.has(name)) listeners.set(name, new Set());
    listeners.get(name)!.add(cb);
    return () => listeners.get(name)!.delete(cb);
  },
  EventsOff: () => {},
};

if (scene === 'running') setTimeout(() => runJob('backup', '_proje').then(() => Bridge.ConfirmPlan(true)), 300);
export {};
