package cortext

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"
)

// openLocalEngine loads the native library named by the env overrides
// (CORTEXT_LIBRARY_PATH, or the release assets) and opens a fresh store.
func openLocalEngine(t *testing.T, name string) *Handle {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping native engine test in short mode")
	}
	ResetLibraryForTest()
	t.Cleanup(ResetLibraryForTest)
	if err := LoadLibrary(); err != nil {
		t.Fatalf("LoadLibrary: %v", err)
	}
	requireEngineV5(t)
	engine, err := New(filepath.Join(t.TempDir(), name), nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(engine.Close)
	return engine
}

// requireEngineV5 skips when the loaded native predates the reference-time
// options layout (an older CORTEXT_LIBRARY_PATH build). With
// CORTEXT_REQUIRE_ENGINE_V5=1, as in CI, that is a failure instead.
func requireEngineV5(t *testing.T) {
	t.Helper()
	if initNativeProcessOptions().StructSize < unsafe.Sizeof(nativeProcessOptions{}) {
		skipUnlessV5Required(t, "native %s %s predates engine v5 reference time", LibraryPath(), Version())
	}
}

func skipUnlessV5Required(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("CORTEXT_REQUIRE_ENGINE_V5") == "1" {
		t.Fatalf("CORTEXT_REQUIRE_ENGINE_V5=1: "+format, args...)
	}
	t.Skipf(format, args...)
}

type packetContent struct {
	mimetype  string
	timestamp uint64
	payload   []byte
}

// packetMemories returns every memory of the given context arrays.
func packetMemories(t *testing.T, ctx map[string]any, keys ...string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, key := range keys {
		raw, ok := ctx[key]
		if !ok {
			t.Fatalf("context has no %s", key)
		}
		list, ok := raw.([]any)
		if !ok {
			t.Fatalf("context %s is %T, want array", key, raw)
		}
		for _, m := range list {
			out = append(out, m.(map[string]any))
		}
	}
	return out
}

func memoryContent(t *testing.T, memory map[string]any) []packetContent {
	t.Helper()
	var out []packetContent
	for _, c := range memory["content"].([]any) {
		entry := c.(map[string]any)
		payload, err := base64.StdEncoding.DecodeString(entry["base64"].(string))
		if err != nil {
			t.Fatalf("content base64: %v", err)
		}
		ts, ok := entry["timestamp"].(float64)
		if !ok {
			t.Fatalf("content entry has no numeric timestamp: %v", entry)
		}
		out = append(out, packetContent{
			mimetype:  entry["mimetype"].(string),
			timestamp: uint64(ts),
			payload:   payload,
		})
	}
	return out
}

// recalls reports whether any memory carries a payload containing needle.
func recalls(t *testing.T, memories []map[string]any, needle string) bool {
	t.Helper()
	for _, memory := range memories {
		for _, c := range memoryContent(t, memory) {
			if bytes.Contains(c.payload, []byte(needle)) {
				return true
			}
		}
	}
	return false
}

func TestNativeProcessOptionsLayoutMatchesCAPI(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("offsets below are the 64-bit C layout")
	}
	var native nativeProcessOptions
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"evidence_budget_bytes", unsafe.Offsetof(native.EvidenceBudgetBytes), 24},
		{"has_reference_ms", unsafe.Offsetof(native.HasReferenceMs), 32},
		{"as_of", unsafe.Offsetof(native.AsOf), 36},
		{"reference_ms", unsafe.Offsetof(native.ReferenceMs), 40},
		{"sizeof", unsafe.Sizeof(native), 48},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}

func TestProcessOptionsCarryTheReferenceTime(t *testing.T) {
	ResetLibraryForTest()
	t.Cleanup(ResetLibraryForTest)
	if err := LoadLibrary(); err != nil {
		t.Skipf("LoadLibrary: %v", err)
	}
	requireEngineV5(t)
	unset, err := makeNativeProcessOptions(&ProcessOptions{Retention: RetentionEphemeral})
	if err != nil {
		t.Fatalf("unset reference: %v", err)
	}
	if unset.HasReferenceMs != 0 || unset.AsOf != 0 || unset.ReferenceMs != 0 {
		t.Fatalf("unset reference = %+v", unset)
	}

	at := time.UnixMilli(1_783_463_360_158)
	native, err := makeNativeProcessOptions(&ProcessOptions{
		Retention: RetentionEphemeral, ReferenceTime: at, AsOf: true,
	})
	if err != nil {
		t.Fatalf("reference time: %v", err)
	}
	if native.HasReferenceMs != 1 || native.AsOf != 1 || native.ReferenceMs != 1_783_463_360_158 {
		t.Fatalf("native reference = %+v", native)
	}
	nearest, err := makeNativeProcessOptions(&ProcessOptions{Retention: RetentionEphemeral, ReferenceTime: at})
	if err != nil {
		t.Fatalf("reference time without as_of: %v", err)
	}
	if nearest.HasReferenceMs != 1 || nearest.AsOf != 0 {
		t.Fatalf("native reference without as_of = %+v", nearest)
	}

	if _, err := makeNativeProcessOptions(&ProcessOptions{AsOf: true}); err == nil {
		t.Fatal("AsOf without ReferenceTime was accepted")
	}
	if _, err := makeNativeProcessOptions(&ProcessOptions{ReferenceTime: time.UnixMilli(-1)}); err == nil {
		t.Fatal("a reference time before the Unix epoch was accepted")
	}
}

func TestProcessOptionsRejectFieldsTheNativeLacks(t *testing.T) {
	previous := fnProcessJSONOptionsInit
	t.Cleanup(func() { fnProcessJSONOptionsInit = previous })
	// An engine that predates the reference-time fields clips struct_size to
	// the evidence-budget layout.
	fnProcessJSONOptionsInit = func(o *nativeProcessOptions) {
		o.StructSize = unsafe.Offsetof(o.HasReferenceMs)
		o.IncludeEmbedding = 1
	}
	if _, err := makeNativeProcessOptions(&ProcessOptions{EvidenceBudgetBytes: 100}); err != nil {
		t.Fatalf("budget on a budget-aware engine: %v", err)
	}
	_, err := makeNativeProcessOptions(&ProcessOptions{ReferenceTime: time.UnixMilli(1)})
	if err == nil || !strings.Contains(err.Error(), "reference time") {
		t.Fatalf("reference time on an older engine: err = %v", err)
	}

	fnProcessJSONOptionsInit = func(o *nativeProcessOptions) {
		o.StructSize = unsafe.Offsetof(o.EvidenceBudgetBytes)
		o.IncludeEmbedding = 1
	}
	_, err = makeNativeProcessOptions(&ProcessOptions{EvidenceBudgetBytes: 100})
	if err == nil || !strings.Contains(err.Error(), "evidence budget") {
		t.Fatalf("budget on an older engine: err = %v", err)
	}
}

func TestProcessOptionsFallBackToTheLegacyRetentionLayout(t *testing.T) {
	previous := fnProcessJSONOptionsInit
	t.Cleanup(func() { fnProcessJSONOptionsInit = previous })
	// Released pre-v5 engines keep struct_size only when it equals their own
	// 24-byte layout and clip anything else to the include_embedding prefix.
	legacy := unsafe.Offsetof(nativeProcessOptions{}.EvidenceBudgetBytes)
	fnProcessJSONOptionsInit = func(o *nativeProcessOptions) {
		if o.StructSize != legacy {
			o.StructSize = unsafe.Offsetof(o.Retention)
		}
		o.IncludeEmbedding = 1
	}
	native, err := makeNativeProcessOptions(&ProcessOptions{Retention: RetentionEphemeral})
	if err != nil {
		t.Fatalf("retention on a pre-v5 engine: %v", err)
	}
	if native.StructSize != legacy || native.Retention != retentionEphemeralC {
		t.Fatalf("native options = %+v, want struct_size %d with Ephemeral retention", native, legacy)
	}
	if nativeIsEngineV5() {
		t.Fatal("a 24-byte options engine was reported as engine v5")
	}

	fnProcessJSONOptionsInit = func(o *nativeProcessOptions) {
		o.StructSize = unsafe.Offsetof(o.Retention)
		o.IncludeEmbedding = 1
	}
	if _, err := makeNativeProcessOptions(&ProcessOptions{Retention: RetentionDurable}); err == nil {
		t.Fatal("retention on an engine without the retention field was accepted")
	}
}

func TestConfigRejectsShiftedFieldsOnPreV5Engines(t *testing.T) {
	if err := checkConfigForNative(&Config{Focus: Ptr(0.3), ReinforcementEnabled: Ptr(false)}, false); err != nil {
		t.Fatalf("fields laid out alike in both engines rejected: %v", err)
	}
	for name, cfg := range map[string]*Config{
		"SequentialEdgesEnabled": {SequentialEdgesEnabled: Ptr(false)},
		"SignalFilterAudio":      {SignalFilterAudio: Ptr(false)},
		"SignalFilterImage":      {SignalFilterImage: Ptr(false)},
		"SignalFilterText":       {SignalFilterText: Ptr(false)},
	} {
		if err := checkConfigForNative(cfg, false); err == nil {
			t.Errorf("%s accepted on a pre-v5 engine", name)
		}
		if err := checkConfigForNative(cfg, true); err != nil {
			t.Errorf("%s rejected on engine v5: %v", name, err)
		}
	}
}

func TestReferenceTimeAsOfHidesLaterMemories(t *testing.T) {
	engine := openLocalEngine(t, "reference.db")
	durable := &ProcessOptions{Retention: RetentionDurable}
	if _, err := engine.ProcessTextWithOptions("The ferry to the island leaves from pier four.", "chat/ferry/old", durable); err != nil {
		t.Fatalf("ProcessText: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	cutoff := time.Now()
	time.Sleep(50 * time.Millisecond)
	if _, err := engine.ProcessTextWithOptions("The ferry to the island now leaves from pier nine.", "chat/ferry/new", durable); err != nil {
		t.Fatalf("ProcessText: %v", err)
	}
	if err := engine.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	query := "Which pier does the ferry to the island leave from?"
	ask := func(options *ProcessOptions) []map[string]any {
		ctx, err := engine.ProcessTextWithOptions(query, "query/public", options)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		return packetMemories(t, ctx, "retrieved_memory")
	}

	plain := ask(&ProcessOptions{Retention: RetentionEphemeral})
	if !recalls(t, plain, "pier four") || !recalls(t, plain, "pier nine") {
		t.Fatal("plain query did not recall both ferry memories")
	}
	nearest := ask(&ProcessOptions{Retention: RetentionEphemeral, ReferenceTime: cutoff})
	if !recalls(t, nearest, "pier four") || !recalls(t, nearest, "pier nine") {
		t.Fatal("reference time without AsOf dropped a memory; nothing should be dropped for being later")
	}
	asOf := ask(&ProcessOptions{Retention: RetentionEphemeral, ReferenceTime: cutoff, AsOf: true})
	if !recalls(t, asOf, "pier four") {
		t.Fatal("AsOf query lost the memory stored before the reference time")
	}
	if recalls(t, asOf, "pier nine") {
		t.Fatal("AsOf query returned a memory stored after the reference time")
	}
}

func TestContentEntriesCarryTheirTimestamps(t *testing.T) {
	engine := openLocalEngine(t, "timestamps.db")
	before := uint64(time.Now().UnixMilli())
	if _, err := engine.ProcessTextWithOptions("The garage door code is 8841.", "chat/garage",
		&ProcessOptions{Retention: RetentionDurable}); err != nil {
		t.Fatalf("ProcessText: %v", err)
	}
	after := uint64(time.Now().UnixMilli())
	ctx, err := engine.ProcessTextWithOptions("What is the garage door code?", "query/public",
		&ProcessOptions{Retention: RetentionEphemeral})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	found := false
	for _, memory := range packetMemories(t, ctx, "retrieved_memory") {
		for _, c := range memoryContent(t, memory) {
			if !bytes.Contains(c.payload, []byte("8841")) {
				continue
			}
			found = true
			if c.timestamp < before || c.timestamp > after {
				t.Fatalf("content timestamp %d outside [%d, %d]", c.timestamp, before, after)
			}
		}
	}
	if !found {
		t.Fatal("garage memory not recalled")
	}
}

func TestProcessTextWithMediaStoresCallerPayload(t *testing.T) {
	engine := openLocalEngine(t, "text-media.db")
	photo := []byte{0xff, 0xd8, 0xff, 0xe0, 9, 8, 7, 0xff, 0xd9}
	durable := &ProcessOptions{Retention: RetentionDurable}

	if _, err := engine.ProcessTextWithMediaAndOptions("A photo of the parking ticket for level 4 spot 27.", "chat/parking",
		&Media{Data: photo, MimeType: "image/jpeg"}, durable); err != nil {
		t.Fatalf("ProcessTextWithMedia image payload: %v", err)
	}
	if _, err := engine.ProcessTextWithMediaAndOptions("The dentist appointment is on Thursday.", "chat/dentist",
		nil, durable); err != nil {
		t.Fatalf("ProcessTextWithMedia empty media: %v", err)
	}
	if _, err := engine.ProcessTextWithMediaAndOptions("Meeting notes.", "chat/notes",
		&Media{Data: []byte("# Standup\nShip the lighthouse release on Friday."), MimeType: "text/markdown"}, durable); err != nil {
		t.Fatalf("ProcessTextWithMedia text payload: %v", err)
	}

	byMime := map[string][]byte{}
	for _, query := range []string{"parking ticket level spot", "dentist appointment day", "lighthouse release"} {
		ctx, err := engine.ProcessTextWithMediaAndOptions(query, "query/public", nil,
			&ProcessOptions{Retention: RetentionEphemeral, EvidenceBudgetBytes: 4096})
		if err != nil {
			t.Fatalf("query %q: %v", query, err)
		}
		for _, memory := range packetMemories(t, ctx, "retrieved_memory") {
			for _, c := range memoryContent(t, memory) {
				if c.timestamp == 0 {
					t.Fatalf("stored content has unknown timestamp: %+v", c)
				}
				byMime[c.mimetype] = append(byMime[c.mimetype], c.payload...)
			}
		}
	}
	if !bytes.Contains(byMime["image/jpeg"], photo) {
		t.Fatalf("image/jpeg payload not stored in place of the text; got mimetypes %v", keys(byMime))
	}
	if !bytes.Contains(byMime["text/plain"], []byte("dentist appointment is on Thursday")) {
		t.Fatal("empty media did not store the canonical text")
	}
	if !bytes.Contains(byMime["text/markdown"], []byte("lighthouse release")) {
		t.Fatal("text/markdown payload not recalled as text")
	}
}

func TestStandingMemoryHoldsDirectivesApart(t *testing.T) {
	engine := openLocalEngine(t, "standing.db")
	durable := &ProcessOptions{Retention: RetentionDurable}
	if _, err := engine.ProcessTextWithOptions("Always answer with the departure pier.", "chat/rules", durable); err != nil {
		t.Fatalf("ProcessText: %v", err)
	}
	if _, err := engine.ProcessTextWithOptions("The ferry to the island leaves at 9 from pier four.", "chat/ferry", durable); err != nil {
		t.Fatalf("ProcessText: %v", err)
	}
	ctx, err := engine.ProcessTextWithOptions("When does the ferry leave?", "query/public",
		&ProcessOptions{Retention: RetentionEphemeral})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	standing := packetMemories(t, ctx, "standing_memory")
	if !recalls(t, standing, "Always answer with the departure pier") {
		t.Fatal("standing directive missing from standing_memory")
	}
	// standing_memory holds only directives; the fact stays in the ranking.
	if recalls(t, standing, "leaves at 9 from pier four") {
		t.Fatal("non-directive fact placed in standing_memory")
	}
	if !recalls(t, packetMemories(t, ctx, "retrieved_memory"), "leaves at 9 from pier four") {
		t.Fatal("ferry fact missing from retrieved_memory")
	}
}

func TestMaterializeModelPlacesLateInteractionBesideAIST(t *testing.T) {
	modelsRoot := localModelsRoot(t)
	manifest, err := loadModelManifest(modelsRoot)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	li, err := findModelAsset(manifest, liModelAssetName)
	if err != nil {
		skipUnlessV5Required(t, "models tree predates the LI reranker: %v", err)
	}
	cache := t.TempDir()
	model, err := materializeModelLocked(cache, modelsRoot)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	beside := filepath.Join(filepath.Dir(model), "AIST-87M-LI.gguf")
	if ok, got := fileHasSHA256(beside, li.SHA256); !ok {
		t.Fatalf("LI model beside AIST at %s: sha256 %q, want %s", beside, got, li.SHA256)
	}
}

func TestModelEnvRejectsMissingLateInteractionPath(t *testing.T) {
	t.Setenv("CORTEXT_AIST_MODEL_PATH", "")
	t.Setenv("CORTEXT_LI_MODEL_PATH", filepath.Join(t.TempDir(), "missing-LI.gguf"))
	if _, err := ensureModelEnv(); err == nil || !strings.Contains(err.Error(), "CORTEXT_LI_MODEL_PATH") {
		t.Fatalf("ensureModelEnv error = %v, want CORTEXT_LI_MODEL_PATH error", err)
	}
}

func TestNewHonorsLateInteractionModelPath(t *testing.T) {
	li := os.Getenv("CORTEXT_LI_MODEL_PATH")
	if li == "" {
		// Materialize the release reranker and point the engine at it.
		modelsRoot := localModelsRoot(t)
		model, err := materializeModelLocked(t.TempDir(), modelsRoot)
		if err != nil {
			t.Fatalf("materialize: %v", err)
		}
		li = filepath.Join(filepath.Dir(model), "AIST-87M-LI.gguf")
		if _, err := os.Stat(li); err != nil {
			skipUnlessV5Required(t, "models tree has no LI reranker: %v", err)
		}
		t.Setenv("CORTEXT_LI_MODEL_PATH", li)
	}
	engine := openLocalEngine(t, "li.db")
	if _, err := engine.ProcessText("Bailey likes tennis balls.", "chat/main"); err != nil {
		t.Fatalf("ProcessText with CORTEXT_LI_MODEL_PATH=%s: %v", li, err)
	}
}

// localModelsRoot resolves a models tree (manifest.json plus assembled files
// or chunks) from CORTEXT_ASSETS_DIR, either the models tree itself or a
// release tree containing models/, else from the downloaded release assets.
func localModelsRoot(t *testing.T) string {
	t.Helper()
	dir := strings.TrimSpace(os.Getenv("CORTEXT_ASSETS_DIR"))
	if dir == "" {
		if testing.Short() {
			t.Skip("skipping release asset download in short mode")
		}
		root, err := EnsureAssets()
		if err != nil {
			t.Fatalf("EnsureAssets: %v", err)
		}
		return modelsRootFromAssets(root)
	}
	for _, root := range []string{dir, filepath.Join(dir, "models")} {
		if st, err := os.Stat(filepath.Join(root, "manifest.json")); err == nil && !st.IsDir() {
			return root
		}
	}
	t.Skipf("no models manifest under %s", dir)
	return ""
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
