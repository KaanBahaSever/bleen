# Changelog

## 0.2.0-beta.1

First public beta. / İlk herkese açık beta.

**Downloads / İndirmeler**

| File | For |
|---|---|
| `bleen-windows-amd64.exe` | Windows 10/11 desktop app |
| `bleen-windows7-8-amd64.exe` | Windows 7/8.1 desktop app (needs the WebView2 runtime, version 109) |
| `bleenctl-*` | Command-line tool for Windows, Linux and macOS |

No installer: download and run. The exe is not code-signed yet, so Windows SmartScreen may warn the first time ("More info" → "Run anyway").
Kurulum yok: indir ve çalıştır. Exe henüz imzalı olmadığı için Windows SmartScreen ilk seferde uyarabilir ("Ek bilgi" → "Yine de çalıştır").

**What's in it**

- Full backup first, then only new and changed files; deletions tracked
- Plain ZIP archives with a manifest; restorable without bleen
- Network folders (`\\SERVER\Share\Folder`), several locations, several backup disks in turn
- Restore any day, everything or selected files, every file verified with SHA-256
- Optional password encryption (age); file names are hidden too
- Old backups: a fresh full backup every N backups, keep the last N
- Pause/continue, preflight summary, mass-change (ransomware) guard, source-empty guard
- Optional daily backup via Windows Task Scheduler (off by default; bleen never runs in the background otherwise)
- Turkish and English, light and dark theme

**Known limits**

- macOS and Linux desktop apps are not released yet (bleenctl works there)
- Automatic backups are not available for encrypted disks
- The Windows 7/8.1 edition is built with a community-patched Go toolchain ([XTLS/go-win7](https://github.com/XTLS/go-win7)) and has not been tested on real Windows 7/8.1 hardware yet; please report problems
