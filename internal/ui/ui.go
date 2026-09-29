// Package ui wires the app layer to Wails: one window, typed bindings and
// events. All Wails-specific code lives here (ADR-0002).
package ui

import (
	"context"
	"io/fs"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/kaanbahasever/bleen/internal/app"
	"github.com/kaanbahasever/bleen/internal/platform"
)

// Run opens the window and blocks until it closes. Closing the window ends
// the process: bleen never keeps running in the background.
func Run(assets fs.FS) error {
	a, err := app.New()
	if err != nil {
		return err
	}
	b := &Bridge{a: a}
	lc := &lifecycle{b: b}
	return wails.Run(&options.App{
		Title:            "bleen",
		Width:            1080,
		Height:           720,
		MinWidth:         880,
		MinHeight:        600,
		BackgroundColour: &options.RGBA{R: 251, G: 250, B: 247, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        lc.startup,
		OnBeforeClose:    lc.beforeClose,
		OnShutdown:       lc.shutdown,
		Bind:             []any{b},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "app.bleen.desktop",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				if lc.ctx != nil {
					runtime.WindowUnminimise(lc.ctx)
					runtime.WindowShow(lc.ctx)
				}
			},
		},
		Windows: &windows.Options{Theme: windows.SystemDefault},
	})
}

type lifecycle struct {
	b   *Bridge
	ctx context.Context
}

func (l *lifecycle) startup(ctx context.Context) {
	l.ctx = ctx
	l.b.ctx = ctx
	l.b.a.Emit = func(name string, data any) { runtime.EventsEmit(ctx, name, data) }
	l.b.a.Start(ctx)
}

// beforeClose asks before stopping a running backup. Returning true keeps
// the window open.
func (l *lifecycle) beforeClose(ctx context.Context) bool {
	if !l.b.a.Busy() {
		return false
	}
	tr := isTurkish(l.b.a.GetState().Language)
	title, msg, stop, keep := "bleen", "A backup is running. Stop it and quit?\n\nYour earlier backups are not affected.",
		"Stop & quit", "Keep backing up"
	if tr {
		msg = "Bir yedekleme sürüyor. Durdurup çıkılsın mı?\n\nÖnceki yedeklerin etkilenmez."
		stop, keep = "Durdur ve çık", "Yedeklemeye devam et"
	}
	res, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
		Type: runtime.QuestionDialog, Title: title, Message: msg,
		Buttons: []string{keep, stop}, DefaultButton: keep, CancelButton: keep,
	})
	if err != nil {
		return true
	}
	// Windows returns "Yes"/"No" for two-button question dialogs.
	if res == stop || res == "No" {
		l.b.a.Cancel()
		deadline := time.Now().Add(10 * time.Second)
		for l.b.a.Busy() && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		return false
	}
	return true
}

func (l *lifecycle) shutdown(context.Context) { l.b.a.Shutdown() }

func isTurkish(lang string) bool {
	if lang == "tr" {
		return true
	}
	if lang == "en" {
		return false
	}
	return strings.HasPrefix(strings.ToLower(platform.UILanguage()), "tr")
}
