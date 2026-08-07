package cortext

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

var (
	libOnce   sync.Once
	libHandle uintptr
	libErr    error
	libPath   string
)

func platformTag() string {
	var osTag string
	switch runtime.GOOS {
	case "darwin":
		osTag = "macos"
	case "linux":
		osTag = "linux"
	case "windows":
		osTag = "windows"
	default:
		osTag = runtime.GOOS
	}

	var arch string
	switch runtime.GOARCH {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "aarch64"
	default:
		arch = runtime.GOARCH
	}
	return osTag + "-" + arch
}

func libraryNameForTag(tag string) string {
	switch {
	case len(tag) >= 5 && tag[:5] == "macos":
		return "libcortext.dylib"
	case len(tag) >= 7 && tag[:7] == "windows":
		return "cortext.dll"
	default:
		return "libcortext.so"
	}
}

func resolveLibraryPath() (string, error) {
	if env := os.Getenv("CORTEXT_LIBRARY_PATH"); env != "" {
		if st, err := os.Stat(env); err != nil || st.IsDir() {
			return "", fmt.Errorf("CORTEXT_LIBRARY_PATH is missing or not a file: %s", env)
		}
		abs, err := filepath.Abs(env)
		if err != nil {
			return env, nil
		}
		return abs, nil
	}

	root, err := EnsureAssets()
	if err != nil {
		return "", err
	}
	path := nativeLibraryFromAssets(root)
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return "", fmt.Errorf("release assets missing native library for %s at %s", platformTag(), path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path, nil
	}
	return abs, nil
}

func loadLibrary() error {
	libOnce.Do(func() {
		path, err := resolveLibraryPath()
		if err != nil {
			libErr = fmt.Errorf(
				"could not load Cortext shared library for %s: %w\nset CORTEXT_LIBRARY_PATH, CORTEXT_ASSETS_DIR, or allow download of github.com/augmem/cortext.cpp release assets v%s",
				platformTag(), err, assetsVersion(),
			)
			return
		}
		handle, err := openLibrary(path)
		if err != nil {
			libErr = fmt.Errorf("load library %s: %w", path, err)
			return
		}
		if err := registerFuncs(handle); err != nil {
			_ = closeLibrary(handle)
			libErr = err
			return
		}
		libHandle = handle
		libPath = path
	})
	return libErr
}

// LoadLibrary loads the native Cortext shared library if it is not already loaded.
// New and Version call this automatically. This may download release assets.
func LoadLibrary() error {
	return loadLibrary()
}

// LibraryPath returns the absolute path that was successfully loaded, if any.
func LibraryPath() string {
	return libPath
}

// ResetLibraryForTest clears the process-wide library singleton (tests only).
func ResetLibraryForTest() {
	if libHandle != 0 {
		_ = closeLibrary(libHandle)
	}
	libOnce = sync.Once{}
	libHandle = 0
	libErr = nil
	libPath = ""
	hasProcessTextMedia = false
	fnProcessTextMediaOpts = nil
	ResetAssetsForTest()
}
