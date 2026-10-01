# Changelog

## 0.3.0-beta.2

Fixes from a second full review. Please update. / İkinci kapsamlı incelemeden gelen düzeltmeler; lütfen güncelleyin.

- **Fixed (data):** "Remove the lock" no longer takes a disk away from a bleen that is still running on this computer (such as a scheduled backup), but a lock left by a crash is still recognised as stale even when Windows has reused its process number; a catalog is never saved after another bleen took the disk over; retention is no longer held back forever by a file that can never be stored (for example a video too large for FAT32); files with names of up to 255 characters restore again; recovery and rebuild handle backups with 100 or more parts, never move the healthy parts of a backup aside because one part can't be read, and are not blocked by files left behind by retention; rebuilding works after a source folder on the disk was renamed, and `bleenctl rebuild-catalog` now works when the catalog can't be read; incrementals stay in place when their full backup is incomplete; restored files and exported ZIPs are readable by other users again on macOS and Linux
- **Fixed (restore without bleen):** the documented order is now "for each archive, first delete what DELETED.txt lists, then extract"; a file that became a folder (or the reverse) is listed so this works; archive names always sort in backup order, even after a time-zone change; ZIP times are local time in every tool; entries of 4 GB and more carry the ZIP64 field in their local header
- **Fixed (app):** a run where every location failed says so instead of "All done"; scheduled backups use whichever of your rotating disks is plugged in, report reset settings, and exit with an error code when something failed; a missing destination drive is reported as such, not as a missing backup disk; a location added to config.toml by hand is backed up; the history screen never shows one day's files under another day's date; events reach the screen in order
- **Fixed (CLI):** `restore --zip` with `--overwrite`, and `--id` with `--at`, are refused instead of being ignored; `list` shows start times, as `--at` uses
- **Fixed (site):** the English version of the website works again

## 0.3.0-beta.1

Big update after a full review, with many data-safety fixes. Please update. / Kapsamlı incelemenin ardından büyük güncelleme; birçok veri güvenliği düzeltmesi içerir. Lütfen güncelleyin.

**Downloads / İndirmeler**

| File | For |
|---|---|
| `bleen-windows-amd64.exe` | Windows 10/11 |
| `bleen-windows7-8-amd64.exe` | Windows 7/8.1 (needs the WebView2 runtime, version 109) |
| `bleen-macos-universal.zip` | macOS 12+ (Intel and Apple Silicon) |
| `bleen-linux-amd64.tar.gz` | Linux (GTK 3, WebKitGTK 4.1) |
| `bleenctl-*` | Command-line tool |

Not code-signed yet: Windows SmartScreen may warn ("More info" → "Run anyway"); on macOS, right-click the app → Open the first time.
Henüz imzalı değil: Windows SmartScreen uyarabilir ("Ek bilgi" → "Yine de çalıştır"); macOS'ta ilk seferde uygulamaya sağ tıklayıp "Aç"ı seç.

- **New:** save any day (everything or chosen files) as one ZIP file, from the History screen or with `bleenctl restore --zip`; `bleenctl restore --only` restores chosen files and folders
- **New:** macOS and Linux desktop apps; connect to a network share with a user name and password (Windows); optional automatic backup on macOS (launchd) and Linux (systemd)
- **Changed:** no more README.txt files in the archives or on the backup disk
- **Fixed (data):** moved files are stored again, so hand restores are complete; an unreadable or online-only (OneDrive) file is never recorded as deleted; OneDrive files are backed up; full backups get the empty-folder and mass-change guards; old backups are kept while the newest full backup skipped files; FAT32 disks no longer fail on very large files
- **Fixed (disk):** an unreadable catalog is never silently replaced; a stale lock from a crashed bleen is removed; a different bleen disk at the same drive letter is detected; recovery works for folder names with [ ]; nothing can be restored into the backup folder itself
- **Fixed (app):** the quit dialog's Yes/No; "Restore latest state" restores the latest backup; a double click can't approve the next warning; messages no longer say "verified" when there were problems
- **Tests:** every day of a month of random changes is restored by id, by date, as a ZIP, partly, and by hand with an unzip tool, on plain, encrypted, split and pruned disks, before and after rebuilding the catalog; a damaged archive never restores wrong data

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
