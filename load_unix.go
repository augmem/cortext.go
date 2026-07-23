//go:build darwin || freebsd || linux || netbsd

package cortext

import "github.com/ebitengine/purego"

func openLibrary(path string) (uintptr, error) {
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
}

func closeLibrary(handle uintptr) error {
	return purego.Dlclose(handle)
}
