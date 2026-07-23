//go:build windows

package cortext

import "syscall"

func openLibrary(path string) (uintptr, error) {
	// purego does not export Dlopen on Windows; use the Windows loader.
	// Prefer absolute paths so LoadLibrary does not search PATH first.
	handle, err := syscall.LoadLibrary(path)
	return uintptr(handle), err
}

func closeLibrary(handle uintptr) error {
	return syscall.FreeLibrary(syscall.Handle(handle))
}
