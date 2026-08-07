package cortext

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unsafe"
)

func TestPlatformTag(t *testing.T) {
	tag := platformTag()
	if tag == "" {
		t.Fatal("empty platform tag")
	}
	t.Logf("platform tag: %s (GOOS=%s GOARCH=%s)", tag, runtime.GOOS, runtime.GOARCH)
}

func TestEnsureAssetsFromRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network asset download in short mode")
	}
	ResetLibraryForTest()
	t.Setenv("CORTEXT_ASSETS_DIR", "")
	t.Setenv("CORTEXT_LIBRARY_PATH", "")
	cache := t.TempDir()
	t.Setenv("CORTEXT_ASSET_CACHE_DIR", cache)

	root, err := EnsureAssets()
	if err != nil {
		t.Fatalf("EnsureAssets: %v", err)
	}
	lib := nativeLibraryFromAssets(root)
	if st, err := os.Stat(lib); err != nil || st.IsDir() {
		t.Fatalf("expected native library at %s: %v", lib, err)
	}
	t.Logf("assets root=%s lib=%s", root, lib)

	again, err := EnsureAssets()
	if err != nil {
		t.Fatalf("EnsureAssets second: %v", err)
	}
	if again != root {
		t.Fatalf("assets root changed: %s vs %s", again, root)
	}
}

func TestLoadLibraryAndVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network asset download in short mode")
	}
	ResetLibraryForTest()
	t.Setenv("CORTEXT_ASSETS_DIR", "")
	t.Setenv("CORTEXT_LIBRARY_PATH", "")
	cache := t.TempDir()
	t.Setenv("CORTEXT_ASSET_CACHE_DIR", cache)

	if err := LoadLibrary(); err != nil {
		t.Fatalf("LoadLibrary: %v", err)
	}
	v := Version()
	if v == "" {
		t.Fatal("Version() returned empty string")
	}
	path := LibraryPath()
	if path == "" {
		t.Fatal("LibraryPath empty after load")
	}
	if !strings.Contains(path, string(filepath.Separator)+"native"+string(filepath.Separator)) {
		t.Fatalf("expected release-asset native path, got %s", path)
	}
	t.Logf("cortext version: %s path=%s", v, path)
}

func TestValidateImageBuffer(t *testing.T) {
	if err := validateImageBuffer(make([]byte, 12), 2, 2, 3); err != nil {
		t.Fatalf("valid image rejected: %v", err)
	}
	if err := validateImageBuffer(make([]byte, 10), 1000, 1000, 3); err == nil {
		t.Fatal("short image buffer was accepted")
	}
	if err := validateImageBuffer(nil, 0, 1, 3); err == nil {
		t.Fatal("non-positive dimensions were accepted")
	}
}

func TestMakeNativeMediaPreservesArbitraryBinaryAndMIME(t *testing.T) {
	data := []byte{0x00, 0xff, 0x01, 0x7f}
	native, keep := makeNativeMedia(&Media{Data: data, MimeType: "application/octet-stream"})
	if keep == nil {
		t.Fatal("media keep-alive was nil")
	}
	if native.Data == nil || native.Size != uintptr(len(data)) {
		t.Fatalf("native media data = %p size %d, want non-nil size %d", native.Data, native.Size, len(data))
	}
	got := unsafe.Slice(native.Data, int(native.Size))
	if string(got) != string(data) {
		t.Fatalf("native media data = %x, want %x", got, data)
	}
	if native.MimeType == nil || goStringFromC(uintptr(unsafe.Pointer(native.MimeType))) != "application/octet-stream" {
		t.Fatalf("native media MIME type was not preserved")
	}
}

func TestRegisterOptionalProcessTextMedia(t *testing.T) {
	previous := fnProcessTextMediaOpts
	t.Cleanup(func() { fnProcessTextMediaOpts = previous })

	if registerOptionalProcessTextMedia(func() { panic("missing symbol") }) {
		t.Fatal("missing optional symbol was reported as available")
	}
	if fnProcessTextMediaOpts != nil {
		t.Fatal("missing optional symbol left a callable function")
	}

	fn := func(uintptr, string, string, *nativeMedia, *nativeProcessOptions) uintptr { return 0 }
	if !registerOptionalProcessTextMedia(func() { fnProcessTextMediaOpts = fn }) {
		t.Fatal("available optional symbol was reported as unavailable")
	}
	if fnProcessTextMediaOpts == nil {
		t.Fatal("available optional symbol did not register")
	}
}

func TestProcessTextWithMediaReportsUnsupportedNative(t *testing.T) {
	previousCapability := hasProcessTextMedia
	previousFn := fnProcessTextMediaOpts
	t.Cleanup(func() {
		hasProcessTextMedia = previousCapability
		fnProcessTextMediaOpts = previousFn
	})
	hasProcessTextMedia = false
	fnProcessTextMediaOpts = nil

	h := &Handle{ptr: 1}
	_, err := h.ProcessTextWithMedia("text", "source", nil)
	if err == nil || err.Error() != "native library does not support text media processing" {
		t.Fatalf("ProcessTextWithMedia error = %v, want unsupported error", err)
	}
}

func TestConfigFieldsDistinguishOmittedAndExplicitZero(t *testing.T) {
	cfg := Config{Focus: Ptr(0.0), AffectInterrupt: Ptr(false)}
	if cfg.Focus == nil || *cfg.Focus != 0.0 {
		t.Fatal("explicit zero focus was not preserved")
	}
	if cfg.Sensitivity != nil {
		t.Fatal("omitted sensitivity should remain nil and use the native default")
	}
	if cfg.AffectInterrupt == nil || *cfg.AffectInterrupt {
		t.Fatal("explicit false toggle was not preserved")
	}
}

func TestMaterializeModelFromReleaseChunks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network asset download in short mode")
	}
	ResetLibraryForTest()
	t.Setenv("CORTEXT_ASSETS_DIR", "")
	t.Setenv("CORTEXT_LIBRARY_PATH", "")
	assetCache := t.TempDir()
	t.Setenv("CORTEXT_ASSET_CACHE_DIR", assetCache)

	cache := t.TempDir()
	t.Setenv("CORTEXT_MODEL_CACHE_DIR", cache)
	materializedOnce.Delete(cache)
	if abs, err := filepath.Abs(cache); err == nil {
		materializedOnce.Delete(abs)
	}

	model, err := MaterializeModel(filepath.Join(cache, "memory.sqlite"))
	if err != nil {
		t.Fatalf("MaterializeModel: %v", err)
	}
	st, err := os.Stat(model)
	if err != nil {
		t.Fatalf("stat model: %v", err)
	}
	if st.Size() < 100_000_000 {
		t.Fatalf("model size unexpectedly small: %d", st.Size())
	}
	sum, err := sha256File(model)
	if err != nil {
		t.Fatalf("hash model: %v", err)
	}
	want := "bf4c49954eccc65183f1a97e44606e86c7ee5a4fea500457124b687a3ec97898"
	if sum != want {
		t.Fatalf("model sha256 %s, want %s", sum, want)
	}
}

func TestNewProcessEmbed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network asset download in short mode")
	}
	ResetLibraryForTest()
	t.Setenv("CORTEXT_ASSETS_DIR", "")
	t.Setenv("CORTEXT_LIBRARY_PATH", "")
	t.Setenv("CORTEXT_AIST_MODEL_PATH", "")
	cache := t.TempDir()
	t.Setenv("CORTEXT_ASSET_CACHE_DIR", cache)

	if err := LoadLibrary(); err != nil {
		t.Fatalf("LoadLibrary: %v", err)
	}
	t.Logf("using library %s version %s", LibraryPath(), Version())

	engine, err := New(":memory:", nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer engine.Close()

	ctx, err := engine.ProcessText("Bailey likes tennis balls.", "chat/main")
	if err != nil {
		t.Fatalf("ProcessText: %v", err)
	}
	if ctx == nil {
		t.Fatal("ProcessText returned nil context")
	}
	t.Logf("process keys: %d", len(ctx))

	embedding, err := engine.EmbedText("embed without storing")
	if err != nil {
		t.Fatalf("EmbedText: %v", err)
	}
	if len(embedding) == 0 {
		t.Fatal("empty embedding")
	}
	t.Logf("embedding dim: %d", len(embedding))

	if err := engine.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if err := engine.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
}

func TestCGONotRequired(t *testing.T) {
	if os.Getenv("CGO_ENABLED") == "1" {
		t.Log("CGO_ENABLED=1 for this process; package still uses purego at runtime")
	}
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
