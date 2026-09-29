<p align="center"><img src="assets/brand/bleen-mascot.svg" width="96" alt=""></p>

<h1 align="center">bleen</h1>

<p align="center">Dated, verified backups of your folders, on a disk you own.</p>

- Full backup the first time, only changes after that
- Plain ZIP files you can open without bleen
- Works with network folders like `\\SERVER\Root\_proje`
- Restore any day in one step

> Early development (v0.1). Windows first; macOS and Linux builds are in progress.

**Desktop app** (Go 1.26+, Node 22+, [Wails v2 CLI](https://wails.io)):

```bash
cd cmd/bleen && wails build
```

**Command line**:

```bash
go build -o bleenctl ./cmd/bleenctl
bleenctl init E:\bleen
bleenctl backup \\SERVER\Root\_proje --vault E:\bleen
bleenctl restore _proje --at 2026-09-27 --to D:\Restore --vault E:\bleen
```

[Architecture](docs/architecture.md) · [Apache-2.0](LICENSE)
