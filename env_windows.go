//go:build windows

package cortext

import (
	"fmt"
	"os"
	"sync"
	"syscall"
	"unsafe"
)

// On Windows, cortext.dll (and many CRT-linked natives) read the environment
// through the C runtime's getenv, which does not always observe process-wide
// changes made only via SetEnvironmentVariable / os.Setenv. Push updates through
// both the Win32 process environment and the UCRT/MSVCRT putenv entry points.

var (
	crtEnvOnce sync.Once
	crtPutenv  *syscall.LazyProc
	crtPutenvS *syscall.LazyProc
)

func loadCRTEnvProcs() {
	for _, dll := range []string{"ucrtbase.dll", "msvcrt.dll", "api-ms-win-crt-environment-l1-1-0.dll"} {
		mod := syscall.NewLazyDLL(dll)
		if err := mod.Load(); err != nil {
			continue
		}
		if p := mod.NewProc("_putenv_s"); p.Find() == nil {
			crtPutenvS = p
		}
		if p := mod.NewProc("_putenv"); p.Find() == nil {
			crtPutenv = p
		}
		if crtPutenvS != nil || crtPutenv != nil {
			return
		}
	}
}

func setNativeEnv(key, value string) error {
	if err := os.Setenv(key, value); err != nil {
		return err
	}
	return crtSetEnv(key, value)
}

func unsetNativeEnv(key string) error {
	_ = os.Unsetenv(key)
	return crtSetEnv(key, "")
}

func crtSetEnv(key, value string) error {
	crtEnvOnce.Do(loadCRTEnvProcs)

	if crtPutenvS != nil {
		k, err := syscall.BytePtrFromString(key)
		if err != nil {
			return err
		}
		v, err := syscall.BytePtrFromString(value)
		if err != nil {
			return err
		}
		r, _, callErr := crtPutenvS.Call(uintptr(unsafe.Pointer(k)), uintptr(unsafe.Pointer(v)))
		if r != 0 {
			if callErr != syscall.Errno(0) {
				return fmt.Errorf("crt _putenv_s %s: %w", key, callErr)
			}
			return fmt.Errorf("crt _putenv_s %s failed", key)
		}
		return nil
	}

	if crtPutenv != nil {
		entry := key + "=" + value
		p, err := syscall.BytePtrFromString(entry)
		if err != nil {
			return err
		}
		r, _, callErr := crtPutenv.Call(uintptr(unsafe.Pointer(p)))
		if r != 0 {
			if callErr != syscall.Errno(0) {
				return fmt.Errorf("crt _putenv %s: %w", key, callErr)
			}
			return fmt.Errorf("crt _putenv %s failed", key)
		}
		return nil
	}

	// Fall back to process env only; getenv may still work on some CRTs.
	return nil
}
