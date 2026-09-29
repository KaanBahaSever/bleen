// Package cli implements bleenctl, the headless command-line interface to
// the same engine and vault format the desktop app uses.
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kaanbahasever/bleen/internal/catalog"
	"github.com/kaanbahasever/bleen/internal/engine"
	"github.com/kaanbahasever/bleen/internal/source"
	"github.com/kaanbahasever/bleen/internal/vault"
)

// Version is set at build time with -ldflags "-X .../internal/cli.Version=v0.1.0".
var Version = "dev"

// Execute runs bleenctl and returns the process exit code.
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	root := newRoot()
	if err := root.ExecuteContext(ctx); err != nil {
		var e *engine.Error
		if errors.As(err, &e) {
			fmt.Fprintf(os.Stderr, "error [%s]: %v\n", e.Code, e)
		} else {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		return 1
	}
	return 0
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "bleenctl",
		Short:         "bleen backups from the command line",
		Long:          "bleenctl makes dated, verified ZIP backups of folders (including network shares) on a disk you own.",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().String("vault", os.Getenv("BLEEN_VAULT"), "backup folder on the target disk (or set BLEEN_VAULT)")
	root.PersistentFlags().Bool("break-lock", false, "take over a vault left locked by a crashed bleen")
	root.AddCommand(initCmd(), backupCmd(), listCmd(), restoreCmd(), verifyCmd(), rebuildCmd())
	return root
}

func openVault(cmd *cobra.Command) (*vault.Vault, error) {
	p, _ := cmd.Flags().GetString("vault")
	if p == "" {
		return nil, errors.New("--vault is required (the bleen folder on your backup disk)")
	}
	brk, _ := cmd.Flags().GetBool("break-lock")
	v, err := vault.Open(p, vault.OpenOptions{BreakLock: brk})
	if err != nil {
		var le *vault.LockedError
		if errors.As(err, &le) {
			return nil, fmt.Errorf("%w\nif no other bleen is running, retry with --break-lock", err)
		}
		if errors.Is(err, vault.ErrNotVault) {
			return nil, fmt.Errorf("%w; create one with: bleenctl init %s", err, p)
		}
		return nil, err
	}
	for _, r := range v.Recovered {
		fmt.Println("repaired:", r)
	}
	return v, nil
}

func initCmd() *cobra.Command {
	var label string
	c := &cobra.Command{
		Use:   "init <folder>",
		Short: "Create a bleen backup folder on a disk",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := vault.Create(args[0], label, "bleenctl "+Version)
			if err != nil {
				return err
			}
			defer v.Close()
			fmt.Printf("Backup folder ready: %s (%s)\n", v.Root, v.Meta.Label)
			return nil
		},
	}
	c.Flags().StringVar(&label, "label", "", "friendly name for the disk")
	return c
}

func backupCmd() *cobra.Command {
	var (
		name, excludeFile    string
		full, yes, allowMass bool
		noDefaults           bool
		excludes             []string
	)
	c := &cobra.Command{
		Use:   "backup <folder>...",
		Short: "Back up folders (full the first time, then only changes)",
		Example: `  bleenctl backup '\\SERVER\Root\_proje' --vault E:\bleen
  bleenctl backup ~/Documents --vault /Volumes/Blue/bleen --exclude '*.bak'`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := openVault(cmd)
			if err != nil {
				return err
			}
			defer v.Close()
			ex := append([]string{}, excludes...)
			if !noDefaults {
				ex = append(ex, source.DefaultExcludes...)
			}
			if excludeFile != "" {
				b, err := os.ReadFile(excludeFile)
				if err != nil {
					return err
				}
				ex = append(ex, strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")...)
			}
			for i, a := range args {
				abs, err := filepath.Abs(a)
				if err != nil {
					return err
				}
				if len(args) > 1 {
					fmt.Printf("\n[%d/%d] %s\n", i+1, len(args), abs)
				}
				p := newPrinter()
				opt := engine.BackupOptions{
					Name:     name,
					Full:     full,
					Progress: p,
					Confirm:  confirmPlan(yes, allowMass),
				}
				rep, err := engine.Backup(cmd.Context(), v, source.NewLocal(abs, ex), opt)
				p.done()
				if err != nil {
					return err
				}
				printBackupReport(rep)
			}
			return nil
		},
	}
	f := c.Flags()
	f.StringVar(&name, "name", "", "name for a folder backed up for the first time")
	f.BoolVar(&full, "full", false, "start a fresh full backup")
	f.BoolVarP(&yes, "yes", "y", false, "don't ask before starting")
	f.BoolVar(&allowMass, "allow-mass-change", false, "with --yes: continue even if unusually many files changed")
	f.StringArrayVar(&excludes, "exclude", nil, "skip files matching a pattern (repeatable), e.g. '*.bak' or 'cache/'")
	f.StringVar(&excludeFile, "exclude-file", "", "read exclude patterns from a file, one per line")
	f.BoolVar(&noDefaults, "no-default-excludes", false, "don't skip Office lock files, Thumbs.db, desktop.ini, …")
	return c
}

func confirmPlan(yes, allowMass bool) func(*engine.Plan) bool {
	return func(p *engine.Plan) bool {
		if yes {
			if p.MassChange && !allowMass {
				fmt.Printf("\nStopped: %.0f%% of files changed or were deleted. This can mean ransomware or a wrong folder.\n"+
					"Check the folder, then rerun with --allow-mass-change.\n", p.ChangedRatio*100)
				return false
			}
			return true
		}
		if !isTerminal(os.Stdin) {
			return !p.MassChange
		}
		if p.MassChange {
			fmt.Printf("\n⚠  Unusually many changes: %.0f%% of files changed or were deleted.\n"+
				"   This can mean ransomware or a wrong folder. Type 'yes' to continue: ", p.ChangedRatio*100)
			return readLine() == "yes"
		}
		fmt.Print("Start? [Y/n] ")
		a := strings.ToLower(readLine())
		return a == "" || a == "y" || a == "yes" || a == "e" || a == "evet"
	}
}

func printBackupReport(r *engine.Report) {
	if r.NothingToDo {
		fmt.Println("Nothing changed since the last backup. Your backup is still fresh.")
		printIssues(r.Issues)
		return
	}
	fmt.Printf("✓ %d files backed up (%d already on disk, stored as references)\n", r.FilesStored+r.FilesDeduped, r.FilesDeduped)
	fmt.Printf("✓ %s archived (%s read)\n", size(r.BytesStored), size(r.BytesSource))
	if r.Verified {
		fmt.Println("✓ Archive verified (SHA-256)")
	}
	fmt.Println("✓ Catalog updated")
	for _, a := range r.Archives {
		fmt.Println("  →", a)
	}
	printIssues(r.Issues)
	fmt.Printf("Done in %s.\n", r.Duration.Round(time.Second))
}

func printIssues(issues []archiveIssue) {
	if len(issues) == 0 {
		return
	}
	fmt.Printf("⚠ %d file(s) skipped; they will be tried again next time:\n", len(issues))
	for i, is := range issues {
		if i == 20 {
			fmt.Printf("  … and %d more\n", len(issues)-20)
			break
		}
		fmt.Printf("  %s  [%s] %s\n", is.Path, is.Code, is.Message)
	}
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [source]",
		Short: "List backed-up folders, or the backups of one folder",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := openVault(cmd)
			if err != nil {
				return err
			}
			defer v.Close()
			if len(args) == 0 {
				srcs, err := v.Catalog.Sources()
				if err != nil {
					return err
				}
				if len(srcs) == 0 {
					fmt.Println("No backups yet.")
					return nil
				}
				fmt.Printf("%-20s %-18s %-8s %s\n", "FOLDER", "LAST BACKUP", "BACKUPS", "ORIGIN")
				for _, s := range srcs {
					snaps, _ := v.Catalog.Snapshots(s.ID)
					last := "-"
					if n := len(snaps); n > 0 {
						last = snaps[n-1].FinishedAt.Local().Format("2006-01-02 15:04")
					}
					fmt.Printf("%-20s %-18s %-8d %s (%s)\n", s.Folder, last, len(snaps), s.Origin, s.Host)
				}
				return nil
			}
			src, err := findSource(v, args[0])
			if err != nil {
				return err
			}
			snaps, err := v.Catalog.Snapshots(src.ID)
			if err != nil {
				return err
			}
			fmt.Printf("%s  ←  %s\n\n", src.Folder, src.Origin)
			fmt.Printf("%-9s %-17s %-12s %8s %8s %8s %10s\n", "ID", "DATE", "TYPE", "NEW", "CHANGED", "DELETED", "SIZE")
			for _, s := range snaps {
				fmt.Printf("%-9s %-17s %-12s %8d %8d %8d %10s\n", s.UUID[:8], s.FinishedAt.Local().Format("2006-01-02 15:04"),
					s.Kind, s.FilesNew, s.FilesModified, s.FilesDeleted, size(s.BytesStored))
			}
			return nil
		},
	}
}

func restoreCmd() *cobra.Command {
	var to, at, id string
	var overwrite bool
	c := &cobra.Command{
		Use:   "restore <source> --to <folder>",
		Short: "Restore a folder as it was on a given day",
		Example: `  bleenctl restore _proje --at 2026-09-27 --to D:\Restore\Proje --vault E:\bleen
  bleenctl restore _proje --to D:\Restore\Latest --vault E:\bleen`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if to == "" {
				return errors.New("--to is required")
			}
			v, err := openVault(cmd)
			if err != nil {
				return err
			}
			defer v.Close()
			src, err := findSource(v, args[0])
			if err != nil {
				return err
			}
			before, err := parseAt(at)
			if err != nil {
				return err
			}
			p := newPrinter()
			rep, err := engine.Restore(cmd.Context(), v, src, engine.RestoreOptions{
				Snapshot: id, Before: before, Dest: to, Overwrite: overwrite, Progress: p,
			})
			p.done()
			if err != nil {
				return err
			}
			fmt.Printf("✓ %d files restored to %s (%s), as of %s\n", rep.Files, rep.Dest, size(rep.Bytes),
				rep.Snapshot.FinishedAt.Local().Format("2006-01-02 15:04"))
			fmt.Println("✓ Every file verified (SHA-256)")
			printIssues(rep.Issues)
			return nil
		},
	}
	f := c.Flags()
	f.StringVar(&to, "to", "", "destination folder (should be new or empty)")
	f.StringVar(&at, "at", "latest", "day (2026-09-27), day and time (\"2026-09-27 18:00\") or 'latest'")
	f.StringVar(&id, "id", "", "backup id from 'bleenctl list <source>' (overrides --at)")
	f.BoolVar(&overwrite, "overwrite", false, "allow restoring into a folder that already has files")
	return c
}

func parseAt(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "latest" {
		return time.Time{}, nil
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local); err == nil {
		return t.Add(time.Minute), nil
	}
	for _, layout := range []string{"2006-01-02", "02.01.2006"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t.AddDate(0, 0, 1), nil
		}
	}
	return time.Time{}, fmt.Errorf("can't read date %q; use 2026-09-27, 27.09.2026 or \"2026-09-27 18:00\"", s)
}

func verifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Re-read every backup and check it for damage",
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := openVault(cmd)
			if err != nil {
				return err
			}
			defer v.Close()
			p := newPrinter()
			rep, err := engine.VerifyVault(cmd.Context(), v, p)
			p.done()
			if err != nil {
				return err
			}
			if len(rep.Problems) == 0 {
				fmt.Printf("✓ All %d archives are intact (%s checked).\n", rep.Archives, size(rep.Bytes))
				return nil
			}
			for _, pr := range rep.Problems {
				fmt.Printf("✗ %s  [%s] %s\n", pr.Path, pr.Code, pr.Message)
			}
			return fmt.Errorf("%d archive(s) have problems", len(rep.Problems))
		},
	}
}

func rebuildCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rebuild-catalog",
		Short: "Rebuild the catalog from the archives' own manifests",
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := openVault(cmd)
			if err != nil {
				return err
			}
			defer v.Close()
			if err := v.Rebuild(); err != nil {
				return err
			}
			for _, r := range v.Recovered {
				fmt.Println(r)
			}
			return nil
		},
	}
}

func findSource(v *vault.Vault, key string) (*catalog.Source, error) {
	s, err := v.Catalog.FindSource(key)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, fmt.Errorf("no backed-up folder called %q; see 'bleenctl list'", key)
	}
	return s, nil
}

var stdin = bufio.NewReader(os.Stdin)

func readLine() string {
	s, _ := stdin.ReadString('\n')
	return strings.TrimSpace(s)
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func size(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
