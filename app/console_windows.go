//go:build windows

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// canRelaunchElevated reports whether asking for elevation makes sense:
// only when the current token is not already elevated.
func canRelaunchElevated() bool { return !isElevated() }

// openBrowser hands the address to the shell, so the default browser
// opens it with the rights of the calling user.
func openBrowser(url string) error {
	verb, _ := syscall.UTF16PtrFromString("open")
	target, _ := syscall.UTF16PtrFromString(url)
	return windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL)
}

type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         windows.Handle
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     windows.Handle
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    windows.Handle
	dwHotKey     uint32
	hIcon        windows.Handle
	hProcess     windows.Handle
}

const seeMaskNoCloseProcess = 0x00000040

var procShellExecuteExW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// elevatedConsoleURL relaunches this binary through the UAC prompt with
// a handoff file, waits for it, and returns the address it wrote. The
// browser is then opened by the original, non-elevated process: a
// browser started from an elevated process runs elevated too, which
// Edge and Chrome refuse or warn about.
func elevatedConsoleURL(configPath string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locating the agent binary: %w", err)
	}
	handoff, err := os.CreateTemp("", "senhub-console-*.txt")
	if err != nil {
		return "", fmt.Errorf("creating the handoff file: %w", err)
	}
	handoffPath := handoff.Name()
	_ = handoff.Close()
	defer os.Remove(handoffPath)

	args := []string{"console", "--handoff", handoffPath}
	if configPath != "" {
		args = append(args, "--config-path", configPath)
	}
	for i, a := range args {
		args[i] = syscall.EscapeArg(a)
	}

	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(exe)
	params, _ := syscall.UTF16PtrFromString(strings.Join(args, " "))
	dir, _ := syscall.UTF16PtrFromString(filepath.Dir(exe))
	info := shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: params,
		lpDirectory:  dir,
		nShow:        windows.SW_HIDE,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	ret, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		return "", fmt.Errorf("elevation refused or failed: %w", callErr)
	}
	if info.hProcess != 0 {
		_, _ = windows.WaitForSingleObject(info.hProcess, windows.INFINITE)
		_ = windows.CloseHandle(info.hProcess)
	}
	data, err := os.ReadFile(handoffPath)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return "", fmt.Errorf("the elevated agent did not return the console address; run as administrator: senhub-agent console --print")
	}
	return strings.TrimSpace(string(data)), nil
}
