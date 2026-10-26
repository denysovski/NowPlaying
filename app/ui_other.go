//go:build !windows

package main

import (
	"fmt"
	"os"
)

func fatalBox(msg string) { fmt.Fprintln(os.Stderr, msg) }

var showWindow = func() {}
var quitApp = func() { os.Exit(0) }

func obsRunning() bool { return true }

// Without the Windows window the panel is just a page: open /app in a browser.
func runUI(port int, coverPath func() string, startHidden bool) {
	fmt.Printf("NowPlaying is running.\n  Panel:   http://127.0.0.1:%d/app\n  Overlay: http://127.0.0.1:%d/overlay\n", port, port)
	select {}
}
