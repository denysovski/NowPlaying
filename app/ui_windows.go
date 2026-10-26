//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")

	procMessageBox           = user32.NewProc("MessageBoxW")
	procShowWindow           = user32.NewProc("ShowWindow")
	procIsIconic             = user32.NewProc("IsIconic")
	procSetForegroundWindow  = user32.NewProc("SetForegroundWindow")
	procSetWindowLongPtr     = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProc       = user32.NewProc("CallWindowProcW")
	procDefWindowProc        = user32.NewProc("DefWindowProcW")
	procRegisterClassEx      = user32.NewProc("RegisterClassExW")
	procCreateWindowEx       = user32.NewProc("CreateWindowExW")
	procRegisterWindowMsg    = user32.NewProc("RegisterWindowMessageW")
	procCreatePopupMenu      = user32.NewProc("CreatePopupMenu")
	procAppendMenu           = user32.NewProc("AppendMenuW")
	procTrackPopupMenu       = user32.NewProc("TrackPopupMenu")
	procDestroyMenu          = user32.NewProc("DestroyMenu")
	procGetCursorPos         = user32.NewProc("GetCursorPos")
	procLoadImage            = user32.NewProc("LoadImageW")
	procPostMessage          = user32.NewProc("PostMessageW")
	procShellNotifyIcon      = shell32.NewProc("Shell_NotifyIconW")
	procGetModuleHandle      = kernel32.NewProc("GetModuleHandleW")
	procSetProcessWorkingSet = kernel32.NewProc("SetProcessWorkingSetSize")
	procGetCurrentProcess    = kernel32.NewProc("GetCurrentProcess")
	procDwmSetAttr           = dwmapi.NewProc("DwmSetWindowAttribute")
)

const (
	wmClose        = 0x0010
	wmApp          = 0x8000
	wmTray         = wmApp + 1
	wmShowMain     = wmApp + 2
	wmQuitApp      = wmApp + 3
	wmLButtonUp    = 0x0202
	wmLButtonDbl   = 0x0203
	wmRButtonUp    = 0x0205
	wmContextMenu  = 0x007B
	nimAdd         = 0
	nimModify      = 1
	nimDelete      = 2
	nifMessage     = 0x1
	nifIcon        = 0x2
	nifTip         = 0x4
	nifInfo        = 0x10
	swHide         = 0
	swShow         = 5
	swRestore      = 9
	mfString       = 0x0
	mfSeparator    = 0x800
	tpmReturnCmd   = 0x0100
	tpmRightButton = 0x0002
	gwlpWndProc    = ^uintptr(3) // -4
	hwndMessage    = ^uintptr(2) // -3
)

type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

func u16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

func fatalBox(msg string) {
	procMessageBox.Call(0, uintptr(unsafe.Pointer(u16(msg))), uintptr(unsafe.Pointer(u16("NowPlaying"))), 0x10)
}

func openURL(u string) {
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	}
}

// darkTitleBar paints the native title bar black (Windows 10 20H1+ / 11).
func darkTitleBar(hwnd uintptr) {
	if procDwmSetAttr.Find() != nil {
		return
	}
	on := int32(1)
	procDwmSetAttr.Call(hwnd, 20, uintptr(unsafe.Pointer(&on)), 4) // DWMWA_USE_IMMERSIVE_DARK_MODE
	caption := uint32(0x000A0A0A)
	procDwmSetAttr.Call(hwnd, 35, uintptr(unsafe.Pointer(&caption)), 4) // DWMWA_CAPTION_COLOR (Win 11)
	border := uint32(0x00262626)
	procDwmSetAttr.Call(hwnd, 34, uintptr(unsafe.Pointer(&border)), 4) // DWMWA_BORDER_COLOR (Win 11)
}

// trimMemory hands unused memory back to Windows while we sit in the tray.
func trimMemory() {
	h, _, _ := procGetCurrentProcess.Call()
	procSetProcessWorkingSet.Call(h, ^uintptr(0), ^uintptr(0))
}

// showWindow can be called from any goroutine (e.g. a second copy of the app was started).
var showWindow = func() {}

// quitApp closes the app cleanly (removes the tray icon); safe from any goroutine.
var quitApp = func() { os.Exit(0) }

func runUI(port int, coverPath func() string, startHidden bool) {
	runtime.LockOSThread()
	appURL := fmt.Sprintf("http://127.0.0.1:%d/app", port)

	// No white flash on start, and keep the embedded browser lean.
	os.Setenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR", "FF0A0A0A")
	os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS",
		"--renderer-process-limit=1 --disable-background-networking --disable-component-update "+
			"--disable-extensions --disable-sync --no-first-run --disable-breakpad "+
			"--disable-features=msSmartScreenProtection,msEdgeShoppingUI,Translate,msWebOOUI,SpareRendererForSitePerProcess")

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath:  filepath.Join(os.Getenv("LOCALAPPDATA"), "NowPlaying"),
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "NowPlaying",
			Width:  400,
			Height: 760,
			IconId: 1,
			Center: true,
		},
	})
	if w == nil {
		// WebView2 runtime missing (very rare on Windows 10/11): keep the
		// overlay working and show the panel in the normal browser instead.
		openURL(appURL)
		fatalBox("The NowPlaying window needs Microsoft Edge WebView2, which wasn't found.\n\n" +
			"The overlay still works. The panel was opened in your browser instead.")
		select {}
	}
	defer w.Destroy()

	hwnd := uintptr(w.Window())
	darkTitleBar(hwnd)
	w.SetSize(360, 520, webview2.HintMin)
	w.Bind("openExternal", func(u string) { openURL(u) })
	w.Bind("openFolder", func() {
		// Explorer with the picture selected.
		cmd := exec.Command("explorer")
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer /select,"` + coverPath() + `"`}
		cmd.Start()
	})

	var visible atomic.Bool
	var quitting atomic.Bool
	var hintShown bool
	hinst, _, _ := procGetModuleHandle.Call(0)

	// While hidden the page is unloaded (about:blank), so the window costs
	// almost nothing in the tray; it reloads instantly when opened again.
	show := func() {
		if !visible.Load() {
			w.Navigate(appURL)
			visible.Store(true)
		}
		if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
			procShowWindow.Call(hwnd, swRestore)
		} else {
			procShowWindow.Call(hwnd, swShow)
		}
		procSetForegroundWindow.Call(hwnd)
	}

	// ---- tray icon (own hidden message window)
	var nid notifyIconData
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.UID = 1
	nid.UFlags = nifMessage | nifIcon | nifTip
	nid.UCallbackMessage = wmTray
	nid.HIcon, _, _ = procLoadImage.Call(hinst, 1, 1, 16, 16, 0) // icon resource 1, IMAGE_ICON
	copy(nid.SzTip[:], syscall.StringToUTF16("NowPlaying"))

	balloon := func(title, text string) {
		n := nid
		n.UFlags = nifInfo
		copy(n.SzInfoTitle[:], syscall.StringToUTF16(title))
		copy(n.SzInfo[:], syscall.StringToUTF16(text))
		procShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&n)))
	}

	hide := func() {
		procShowWindow.Call(hwnd, swHide)
		if visible.Load() {
			visible.Store(false)
			w.Navigate("about:blank")
		}
		if !hintShown {
			hintShown = true
			balloon("NowPlaying is still running", "The overlay and !song keep working. Right-click this icon to quit.")
		}
		trimMemory()
	}

	quit := func() {
		quitting.Store(true)
		procShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
		w.Terminate()
	}

	taskbarCreated, _, _ := procRegisterWindowMsg.Call(uintptr(unsafe.Pointer(u16("TaskbarCreated"))))
	trayProc := syscall.NewCallback(func(h, msg, wp, lp uintptr) uintptr {
		switch {
		case msg == wmTray:
			switch lp & 0xffff {
			case wmLButtonUp, wmLButtonDbl:
				show()
			case wmRButtonUp, wmContextMenu:
				menu, _, _ := procCreatePopupMenu.Call()
				procAppendMenu.Call(menu, mfString, 1, uintptr(unsafe.Pointer(u16("Open NowPlaying"))))
				procAppendMenu.Call(menu, mfSeparator, 0, 0)
				procAppendMenu.Call(menu, mfString, 2, uintptr(unsafe.Pointer(u16("Quit"))))
				var pt struct{ X, Y int32 }
				procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
				procSetForegroundWindow.Call(h)
				cmd, _, _ := procTrackPopupMenu.Call(menu, tpmReturnCmd|tpmRightButton, uintptr(pt.X), uintptr(pt.Y), 0, h, 0)
				procDestroyMenu.Call(menu)
				switch cmd {
				case 1:
					show()
				case 2:
					quit()
				}
			}
			return 0
		case msg == wmShowMain:
			show()
			return 0
		case msg == wmQuitApp:
			quit()
			return 0
		case msg == taskbarCreated && taskbarCreated != 0: // Explorer restarted
			procShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
			return 0
		}
		r, _, _ := procDefWindowProc.Call(h, msg, wp, lp)
		return r
	})
	cls := wndClassEx{LpfnWndProc: trayProc, HInstance: hinst, LpszClassName: u16("NowPlayingTray")}
	cls.CbSize = uint32(unsafe.Sizeof(cls))
	procRegisterClassEx.Call(uintptr(unsafe.Pointer(&cls)))
	trayHwnd, _, _ := procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls.LpszClassName)), 0, 0, 0, 0, 0, 0, hwndMessage, 0, hinst, 0)
	nid.HWnd = trayHwnd
	procShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
	defer procShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))

	showWindow = func() { procPostMessage.Call(trayHwnd, wmShowMain, 0, 0) }
	quitApp = func() { procPostMessage.Call(trayHwnd, wmQuitApp, 0, 0) }

	// ---- the X button hides to the tray instead of quitting
	var oldProc uintptr
	mainProc := syscall.NewCallback(func(h, msg, wp, lp uintptr) uintptr {
		if msg == wmClose && !quitting.Load() {
			hide()
			return 0
		}
		r, _, _ := procCallWindowProc.Call(oldProc, h, msg, wp, lp)
		return r
	})
	oldProc, _, _ = procSetWindowLongPtr.Call(hwnd, gwlpWndProc, mainProc)

	if startHidden {
		hintShown = true
		procShowWindow.Call(hwnd, swHide)
		w.Navigate("about:blank")
	} else {
		visible.Store(true)
		w.Navigate(appURL)
	}
	w.Run()
}
