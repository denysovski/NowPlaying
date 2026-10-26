//go:build windows

package main

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// obsRunning reports whether OBS Studio is open (obs64.exe / obs32.exe / obs.exe).
func obsRunning() bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return true // can't tell: don't quit
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		switch strings.ToLower(windows.UTF16ToString(e.ExeFile[:])) {
		case "obs64.exe", "obs32.exe", "obs.exe":
			return true
		}
	}
	return false
}
