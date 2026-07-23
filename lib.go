package cortext

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// Supported platform tags for bundled shared libraries (Zig cross-builds).
var supportedNativeTargets = map[string]struct{}{
	"linux-x86_64":    {},
	"linux-aarch64":   {},
	"macos-x86_64":    {},
	"macos-aarch64":   {},
	"windows-x86_64":  {},
	"windows-aarch64": {},
}

var (
	libOnce   sync.Once
	libHandle uintptr
	libErr    error
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

func packageDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Dir(file)
}

func candidateLibraryPaths() []string {
	names := []string{"libcortext.dylib", "libcortext.so", "cortext.dll", "libcortext.dll"}
	var out []string

	if env := os.Getenv("CORTEXT_LIBRARY_PATH"); env != "" {
		out = append(out, env)
	}

	tag := platformTag()
	pkg := packageDir()
	if pkg != "" {
		if _, ok := supportedNativeTargets[tag]; ok {
			out = append(out, filepath.Join(pkg, "native", tag, libraryNameForTag(tag)))
		}
		for _, name := range names {
			out = append(out, filepath.Join(pkg, name))
		}
	}

	// Sibling cortext checkout (development layout: cortext.go next to cortext).
	if pkg != "" {
		parent := filepath.Dir(pkg)
		for _, dir := range []string{
			filepath.Join(parent, "cortext", "build", "ffi-release"),
			filepath.Join(parent, "cortext", "build", "ffi-release", "lib"),
			filepath.Join(parent, "cortext", "zig-out", "lib"),
			filepath.Join(parent, "cortext", "zig-out", "bin"),
			filepath.Join(parent, "cortext", "bindings", "python", "augmem", "cortext", "native", tag),
		} {
			for _, name := range names {
				out = append(out, filepath.Join(dir, name))
			}
		}
	}

	return out
}

func loadLibrary() error {
	libOnce.Do(func() {
		var lastErr error
		for _, path := range candidateLibraryPaths() {
			if path == "" {
				continue
			}
			if st, err := os.Stat(path); err != nil || st.IsDir() {
				continue
			}
			handle, err := openLibrary(path)
			if err != nil {
				lastErr = fmt.Errorf("load library %s: %w", path, err)
				continue
			}
			if err := registerFuncs(handle); err != nil {
				_ = closeLibrary(handle)
				lastErr = err
				continue
			}
			libHandle = handle
			return
		}

		searched := ""
		for _, p := range candidateLibraryPaths() {
			searched += "\n  " + p
		}
		if lastErr != nil {
			libErr = fmt.Errorf(
				"could not load Cortext shared library for %s: %w\nsearched:%s\nset CORTEXT_LIBRARY_PATH or run scripts/build_native.py",
				platformTag(), lastErr, searched,
			)
			return
		}
		libErr = fmt.Errorf(
			"could not locate Cortext shared library for %s; bundled tags: linux-x86_64, linux-aarch64, macos-x86_64, macos-aarch64, windows-x86_64, windows-aarch64\nsearched:%s\nset CORTEXT_LIBRARY_PATH or run scripts/build_native.py",
			platformTag(), searched,
		)
	})
	return libErr
}

// LoadLibrary loads the native Cortext shared library if it is not already loaded.
// New and Version call this automatically.
func LoadLibrary() error {
	return loadLibrary()
}

// LibraryPath returns the absolute path that was successfully loaded, if any.
func LibraryPath() string {
	if libHandle == 0 {
		return ""
	}
	// purego does not expose the path of an open handle; re-resolve from candidates.
	for _, path := range candidateLibraryPaths() {
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			abs, err := filepath.Abs(path)
			if err == nil {
				return abs
			}
			return path
		}
	}
	return ""
}
