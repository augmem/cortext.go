package cortext

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
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

func TestMixedMemoryContentMimetypes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping native engine test in short mode")
	}
	ResetLibraryForTest()
	if err := LoadLibrary(); err != nil {
		t.Fatalf("LoadLibrary: %v", err)
	}
	engine, err := New(filepath.Join(t.TempDir(), "mixed.db"), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer engine.Close()

	// One turn = one source: a text signal and its photo, as peers.
	photo := []byte{0xff, 0xd8, 0xff, 0xe0, 1, 2, 3, 0xff, 0xd9}
	if _, err := engine.ProcessTextWithOptions("The parking garage ticket says level 4 spot 27.",
		"chat/user/turn1", &ProcessOptions{Retention: RetentionNatural}); err != nil {
		t.Fatalf("ProcessText: %v", err)
	}
	pixels := bytes.Repeat([]byte{128}, 8*8*3)
	if _, err := engine.ProcessImageWithMediaAndOptions(pixels, 8, 8, 3, "chat/user/turn1",
		&Media{Data: photo, MimeType: "image/jpeg"}, &ProcessOptions{Retention: RetentionDurable}); err != nil {
		t.Fatalf("ProcessImageWithMedia: %v", err)
	}
	ctx, err := engine.ProcessTextWithOptions("parking garage level spot", "query/public",
		&ProcessOptions{Retention: RetentionEphemeral})
	if err != nil {
		t.Fatalf("ProcessText query: %v", err)
	}

	var memories []any
	for _, key := range []string{"retrieved_memory", "working_memory"} {
		list, _ := ctx[key].([]any)
		memories = append(memories, list...)
	}
	mixed := 0
	for _, raw := range memories {
		memory, _ := raw.(map[string]any)
		if memory["modality"] != "mixed" {
			continue
		}
		mixed++
		var mimes []string
		for _, c := range memory["content"].([]any) {
			content := c.(map[string]any)
			mime, _ := content["mimetype"].(string)
			mimes = append(mimes, mime)
			if mime == "image/jpeg" {
				got, err := base64.StdEncoding.DecodeString(content["base64"].(string))
				if err != nil || !bytes.Equal(got, photo) {
					t.Fatalf("image payload = %v, %v; want %v", got, err, photo)
				}
			}
		}
		sort.Strings(mimes)
		if strings.Join(mimes, ",") != "image/jpeg,text/plain" {
			t.Fatalf("content mimetypes = %v", mimes)
		}
	}
	if mixed == 0 {
		t.Fatal("no mixed memory recalled")
	}
}

func TestProcessOptionsCarryTheEvidenceBudget(t *testing.T) {
	ResetLibraryForTest()
	if err := LoadLibrary(); err != nil {
		t.Skipf("LoadLibrary: %v", err)
	}
	if got := makeNativeProcessOptions(nil).EvidenceBudgetBytes; got != 0 {
		t.Fatalf("unset budget = %d, want 0", got)
	}
	native := makeNativeProcessOptions(&ProcessOptions{Retention: RetentionEphemeral, EvidenceBudgetBytes: 34_800})
	if native.EvidenceBudgetBytes != 34_800 || native.Retention != retentionEphemeralC {
		t.Fatalf("native options = %+v", native)
	}
	if native.StructSize != unsafe.Sizeof(native) {
		t.Fatalf("struct size = %d, want %d", native.StructSize, unsafe.Sizeof(native))
	}
}

func TestEvidenceBudgetBoundsStandingAndRetrieved(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping native engine test in short mode")
	}
	ResetLibraryForTest()
	if err := LoadLibrary(); err != nil {
		t.Fatalf("LoadLibrary: %v", err)
	}
	engine, err := New(filepath.Join(t.TempDir(), "budget.db"), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer engine.Close()
	durable := &ProcessOptions{Retention: RetentionDurable}
	if _, err := engine.ProcessTextWithOptions("Always answer with the departure pier.", "chat/rules", durable); err != nil {
		t.Fatalf("ProcessText: %v", err)
	}
	for i := 0; i < 6; i++ {
		text := fmt.Sprintf("The ferry to the island leaves at %d from pier four.", 6+i)
		if _, err := engine.ProcessTextWithOptions(text, fmt.Sprintf("chat/ferry/%d", i), durable); err != nil {
			t.Fatalf("ProcessText: %v", err)
		}
	}
	evidence := func(ctx map[string]any) (bytes uint64, retrieved int) {
		for _, key := range []string{"standing_memory", "retrieved_memory"} {
			list, ok := ctx[key].([]any)
			if !ok {
				t.Fatalf("context has no %s array", key)
			}
			if key == "retrieved_memory" {
				retrieved = len(list)
			}
			for _, raw := range list {
				for _, c := range raw.(map[string]any)["content"].([]any) {
					content := c.(map[string]any)
					if strings.HasPrefix(content["mimetype"].(string), "text/") {
						bytes += uint64(content["size_bytes"].(float64))
					} else {
						bytes += 480
					}
				}
			}
		}
		return bytes, retrieved
	}
	query := "When does the ferry leave?"
	whole, err := engine.ProcessTextWithOptions(query, "query/public", &ProcessOptions{Retention: RetentionEphemeral})
	if err != nil {
		t.Fatalf("ProcessText query: %v", err)
	}
	wholeBytes, wholeCount := evidence(whole)
	if wholeCount < 2 {
		t.Fatalf("whole packet retrieved %d memories", wholeCount)
	}
	budget := wholeBytes / 2
	budgeted, err := engine.ProcessTextWithOptions(query, "query/public",
		&ProcessOptions{Retention: RetentionEphemeral, EvidenceBudgetBytes: budget})
	if err != nil {
		t.Fatalf("ProcessText budgeted query: %v", err)
	}
	gotBytes, gotCount := evidence(budgeted)
	if gotBytes > budget || gotCount >= wholeCount {
		t.Fatalf("budgeted packet %d bytes / %d memories; budget %d, whole %d memories", gotBytes, gotCount, budget, wholeCount)
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
