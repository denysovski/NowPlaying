package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// coverFile is the card picture. It lives next to NowPlaying.exe so it's easy
// to swap: any of cover.jpg / .jpeg / .png / .webp / .gif. The built-in one is
// written there on first start.
type coverFile struct {
	folder string
	mu     sync.Mutex
	cur    string
	mod    int64
}

var coverNames = []string{"cover.jpg", "cover.jpeg", "cover.png", "cover.webp", "cover.gif"}

func newCoverFile(exeDir string, web fs.FS) *coverFile {
	c := &coverFile{folder: exeDir}
	if c.find() == "" {
		if data, err := fs.ReadFile(web, "cover.jpg"); err == nil {
			os.WriteFile(filepath.Join(exeDir, "cover.jpg"), data, 0o644)
		}
	}
	c.refresh()
	return c
}

func (c *coverFile) find() string {
	// newest file wins, so dropping in cover.png replaces an old cover.jpg
	best, bestMod := "", int64(0)
	for _, n := range coverNames {
		if st, err := os.Stat(filepath.Join(c.folder, n)); err == nil && !st.IsDir() {
			if m := st.ModTime().UnixMilli(); best == "" || m > bestMod {
				best, bestMod = filepath.Join(c.folder, n), m
			}
		}
	}
	return best
}

// refresh reports whether the picture changed.
func (c *coverFile) refresh() bool {
	p := c.find()
	var mod int64
	if p != "" {
		if st, err := os.Stat(p); err == nil {
			mod = st.ModTime().UnixMilli()
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	changed := p != c.cur || mod != c.mod
	c.cur, c.mod = p, mod
	return changed
}

func (c *coverFile) path() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cur
}

func (c *coverFile) version() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mod
}

// dir returns the file to highlight in Explorer.
func (c *coverFile) dir() string {
	if p := c.path(); p != "" {
		return p
	}
	return strings.TrimRight(c.folder, `\/`)
}

// watch checks the folder every 2 s (a few stat calls, no CPU to speak of).
func (c *coverFile) watch(onChange func(v int64)) {
	for range time.Tick(2 * time.Second) {
		if c.refresh() {
			onChange(c.version())
		}
	}
}
