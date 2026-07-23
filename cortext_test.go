package cortext

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPlatformTag(t *testing.T) {
	tag := platformTag()
	if tag == "" {
		t.Fatal("empty platform tag")
	}
	t.Logf("platform tag: %s (GOOS=%s GOARCH=%s)", tag, runtime.GOOS, runtime.GOARCH)
}

func TestLoadLibraryAndVersion(t *testing.T) {
	if err := LoadLibrary(); err != nil {
		t.Fatalf("LoadLibrary: %v", err)
	}
	v := Version()
	if v == "" {
		t.Fatal("Version() returned empty string")
	}
	t.Logf("cortext version: %s path=%s", v, LibraryPath())
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

func TestMaterializeModelFromChunks(t *testing.T) {
	if !chunksReady() {
		t.Skip("model chunks not present under models/")
	}

	cache := t.TempDir()
	t.Setenv("CORTEXT_MODEL_CACHE_DIR", cache)
	// Clear memoization for this cache key path — use unique env.
	materializedOnce.Delete(cache)

	model, err := MaterializeModel(filepath.Join(cache, "memory.sqlite"))
	if err != nil {
		t.Fatalf("MaterializeModel: %v", err)
	}
	st, err := os.Stat(model)
	if err != nil {
		t.Fatalf("stat model: %v", err)
	}
	if st.Size() != 141491936 {
		t.Fatalf("model size %d, want 141491936", st.Size())
	}
	sum, err := sha256File(model)
	if err != nil {
		t.Fatalf("hash model: %v", err)
	}
	want := "bf4c49954eccc65183f1a97e44606e86c7ee5a4fea500457124b687a3ec97898"
	if sum != want {
		t.Fatalf("model sha256 %s, want %s", sum, want)
	}

	vocab := filepath.Join(cache, "mdbr-leaf-ir", "vocab.txt")
	if _, err := os.Stat(vocab); err != nil {
		t.Fatalf("vocab not copied into cache: %v", err)
	}

	// Second call should hit the verified cache without error.
	again, err := MaterializeModel(filepath.Join(cache, "memory.sqlite"))
	if err != nil {
		t.Fatalf("MaterializeModel second: %v", err)
	}
	if again != model {
		t.Fatalf("cache path changed: %s vs %s", again, model)
	}
}

func TestNewProcessEmbed(t *testing.T) {
	if err := LoadLibrary(); err != nil {
		t.Fatalf("LoadLibrary: %v", err)
	}
	if !chunksReady() && findDefaultAISTModel() == "" && os.Getenv("CORTEXT_AIST_MODEL_PATH") == "" {
		t.Skip("no AIST model available; ship models/chunks or set CORTEXT_AIST_MODEL_PATH")
	}

	cache := t.TempDir()
	t.Setenv("CORTEXT_MODEL_CACHE_DIR", cache)
	materializedOnce.Delete(cache)

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
	// This package must build with CGO_ENABLED=0. The test binary itself is
	// the proof when run under that env (see CI / README).
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
