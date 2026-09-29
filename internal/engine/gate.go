package engine

import (
	"context"
	"sync"
)

// Gate pauses a running backup between files. The zero value is open.
type Gate struct {
	mu     sync.Mutex
	paused bool
	open   chan struct{}
}

func (g *Gate) Pause() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.paused {
		g.paused = true
		g.open = make(chan struct{})
	}
}

func (g *Gate) Resume() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.paused {
		g.paused = false
		close(g.open)
	}
}

func (g *Gate) Paused() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.paused
}

// Wait blocks while the gate is paused.
func (g *Gate) Wait(ctx context.Context) {
	if g == nil {
		return
	}
	g.mu.Lock()
	ch, paused := g.open, g.paused
	g.mu.Unlock()
	if !paused {
		return
	}
	select {
	case <-ch:
	case <-ctx.Done():
	}
}
