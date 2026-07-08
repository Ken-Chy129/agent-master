//go:build !windows

package service

import "errors"

// Stubs so the runtime.GOOS dispatch in service.go compiles on every platform;
// the "windows" arms are unreachable here. Real implementations live in
// service_windows.go.

var errNotWindows = errors.New("windows service management is unavailable on this OS")

func installWindows(string) error { return errNotWindows }
func uninstallWindows() error     { return errNotWindows }
func installedWindows() bool      { return false }
func stopWindows() error          { return errNotWindows }
func restartWindows() error       { return errNotWindows }
