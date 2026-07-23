// Package cortext is a pure-Go (no CGO) binding for the Cortext multimodal
// memory engine. It loads a prebuilt shared library shipped under native/ for
// the current GOOS/GOARCH (Zig cross-builds), or a path from CORTEXT_LIBRARY_PATH.
package cortext

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"
)

// Config overrides native defaults. Nil fields retain their native defaults.
type Config struct {
	Focus                  *float64
	Sensitivity            *float64
	Stability              *float64
	AffectInterrupt        *bool
	AffectRetrieval        *bool
	ReinforcementEnabled   *bool
	ProceduralEnabled      *bool
	SequentialEdgesEnabled *bool
	SignalFilterAudio      *bool
	SignalFilterImage      *bool
	SignalFilterText       *bool
}

// Ptr returns a pointer to value for optional Config fields.
func Ptr[T any](value T) *T { return &value }

// Handle is an open Cortext engine instance.
type Handle struct {
	ptr uintptr
}

// EmbeddingResult is the JSON shape returned by embed helpers.
type EmbeddingResult struct {
	Embedding []float32 `json:"embedding"`
	Dimension int       `json:"dimension"`
}

// Media is optional source media stored alongside audio/image processing.
type Media struct {
	Data     []byte
	MimeType string
}

// Retention controls whether an input is naturally gated, committed, bounded,
// or used only for retrieval. Its zero value is Natural.
type Retention int

const (
	RetentionNatural Retention = iota
	RetentionDurable
	RetentionBoundary
	RetentionEphemeral
)

// ProcessOptions configures JSON process result payloads.
type ProcessOptions struct {
	OmitEmbedding bool
	// Retention is Natural when zero-value / omitted.
	Retention Retention
}

// Version returns the native library version string.
func Version() string {
	if err := loadLibrary(); err != nil {
		return ""
	}
	return fnVersion()
}

// LastError returns the last error produced by this OS thread's C API calls.
// Note: goroutine migration between calls can lose thread-local error text;
// prefer the error returned from each method when available.
func LastError() string {
	if err := loadLibrary(); err != nil {
		return err.Error()
	}
	return fnLastError()
}

// New opens a Cortext instance at dbPath (file path or ":memory:").
// cfg may be nil for all native defaults.
//
// If CORTEXT_AIST_MODEL_PATH is unset, New materializes the bundled AIST model
// from Git-friendly chunks under models/ (hermes-plugin style), or falls back
// to a sibling full .gguf path, then sets the env var for the create call.
func New(dbPath string, cfg *Config) (*Handle, error) {
	if err := loadLibrary(); err != nil {
		return nil, err
	}

	restore, err := ensureModelEnv(dbPath)
	if err != nil {
		return nil, err
	}
	defer restore()

	var nativeCfg nativeConfig
	nativeCfg.StructSize = unsafe.Sizeof(nativeCfg)
	fnConfigInit(&nativeCfg)

	if cfg != nil {
		if cfg.Focus != nil {
			nativeCfg.Focus = *cfg.Focus
		}
		if cfg.Sensitivity != nil {
			nativeCfg.Sensitivity = *cfg.Sensitivity
		}
		if cfg.Stability != nil {
			nativeCfg.Stability = *cfg.Stability
		}
		if cfg.AffectInterrupt != nil {
			nativeCfg.AffectInterrupt = boolToCInt(*cfg.AffectInterrupt)
		}
		if cfg.AffectRetrieval != nil {
			nativeCfg.AffectRetrieval = boolToCInt(*cfg.AffectRetrieval)
		}
		if cfg.ReinforcementEnabled != nil {
			nativeCfg.ReinforcementEnabled = boolToCInt(*cfg.ReinforcementEnabled)
		}
		if cfg.ProceduralEnabled != nil {
			nativeCfg.ProceduralEnabled = boolToCInt(*cfg.ProceduralEnabled)
		}
		if cfg.SequentialEdgesEnabled != nil {
			nativeCfg.SequentialEdgesEnabled = boolToCInt(*cfg.SequentialEdgesEnabled)
		}
		if cfg.SignalFilterAudio != nil {
			nativeCfg.SignalFilterAudioEnabled = boolToCInt(*cfg.SignalFilterAudio)
		}
		if cfg.SignalFilterImage != nil {
			nativeCfg.SignalFilterImageEnabled = boolToCInt(*cfg.SignalFilterImage)
		}
		if cfg.SignalFilterText != nil {
			nativeCfg.SignalFilterTextEnabled = boolToCInt(*cfg.SignalFilterText)
		}
	}

	handle := fnCreateWithConfig(&nativeCfg, dbPath)
	if handle == 0 {
		return nil, lastError()
	}

	result := &Handle{ptr: handle}
	runtime.SetFinalizer(result, func(h *Handle) {
		h.Close()
	})
	return result, nil
}

// Close frees the native handle. Safe to call multiple times.
func (h *Handle) Close() {
	if h == nil || h.ptr == 0 {
		return
	}
	fnFree(h.ptr)
	h.ptr = 0
	runtime.SetFinalizer(h, nil)
}

func (h *Handle) ProcessTextJSON(text string, sourceID string) ([]byte, error) {
	return h.ProcessTextJSONWithOptions(text, sourceID, nil)
}

func (h *Handle) ProcessTextJSONWithOptions(text string, sourceID string, options *ProcessOptions) ([]byte, error) {
	if err := h.require(); err != nil {
		return nil, err
	}
	nativeOptions := makeNativeProcessOptions(options)
	raw := fnProcessTextJSONOpts(h.ptr, text, sourceID, &nativeOptions)
	runtime.KeepAlive(h)
	return takeJSONString(raw)
}

func (h *Handle) ProcessText(text string, sourceID string) (map[string]any, error) {
	return decodeJSONObject(h.ProcessTextJSON(text, sourceID))
}

func (h *Handle) ProcessTextWithOptions(text string, sourceID string, options *ProcessOptions) (map[string]any, error) {
	return decodeJSONObject(h.ProcessTextJSONWithOptions(text, sourceID, options))
}

func (h *Handle) EmbedTextJSON(text string) ([]byte, error) {
	if err := h.require(); err != nil {
		return nil, err
	}
	raw := fnEmbedTextJSON(h.ptr, text)
	runtime.KeepAlive(h)
	return takeJSONString(raw)
}

func (h *Handle) EmbedText(text string) ([]float32, error) {
	result, err := decodeEmbeddingResult(h.EmbedTextJSON(text))
	if err != nil {
		return nil, err
	}
	return result.Embedding, nil
}

func (h *Handle) ProcessAudioJSON(pcm []float32, sourceID string) ([]byte, error) {
	return h.ProcessAudioJSONWithOptions(pcm, sourceID, nil)
}

func (h *Handle) ProcessAudioJSONWithOptions(pcm []float32, sourceID string, options *ProcessOptions) ([]byte, error) {
	if err := h.require(); err != nil {
		return nil, err
	}
	nativeOptions := makeNativeProcessOptions(options)
	raw := fnProcessAudioJSONOpts(h.ptr, float32Ptr(pcm), uintptr(len(pcm)), sourceID, &nativeOptions)
	runtime.KeepAlive(pcm)
	runtime.KeepAlive(h)
	return takeJSONString(raw)
}

func (h *Handle) ProcessAudioWithMediaJSON(pcm []float32, sourceID string, media *Media) ([]byte, error) {
	return h.ProcessAudioWithMediaJSONWithOptions(pcm, sourceID, media, nil)
}

func (h *Handle) ProcessAudioWithMediaJSONWithOptions(pcm []float32, sourceID string, media *Media, options *ProcessOptions) ([]byte, error) {
	if err := h.require(); err != nil {
		return nil, err
	}
	nativeMedia, keep := makeNativeMedia(media)
	nativeOptions := makeNativeProcessOptions(options)
	raw := fnProcessAudioMediaOpts(h.ptr, float32Ptr(pcm), uintptr(len(pcm)), sourceID, &nativeMedia, &nativeOptions)
	runtime.KeepAlive(pcm)
	runtime.KeepAlive(keep)
	runtime.KeepAlive(h)
	return takeJSONString(raw)
}

func (h *Handle) ProcessAudio(pcm []float32, sourceID string) (map[string]any, error) {
	return decodeJSONObject(h.ProcessAudioJSON(pcm, sourceID))
}

func (h *Handle) ProcessAudioWithOptions(pcm []float32, sourceID string, options *ProcessOptions) (map[string]any, error) {
	return decodeJSONObject(h.ProcessAudioJSONWithOptions(pcm, sourceID, options))
}

func (h *Handle) ProcessAudioWithMedia(pcm []float32, sourceID string, media *Media) (map[string]any, error) {
	return decodeJSONObject(h.ProcessAudioWithMediaJSON(pcm, sourceID, media))
}

func (h *Handle) ProcessAudioWithMediaAndOptions(pcm []float32, sourceID string, media *Media, options *ProcessOptions) (map[string]any, error) {
	return decodeJSONObject(h.ProcessAudioWithMediaJSONWithOptions(pcm, sourceID, media, options))
}

func (h *Handle) EmbedAudioJSON(pcm []float32) ([]byte, error) {
	if err := h.require(); err != nil {
		return nil, err
	}
	raw := fnEmbedAudioJSON(h.ptr, float32Ptr(pcm), uintptr(len(pcm)))
	runtime.KeepAlive(pcm)
	runtime.KeepAlive(h)
	return takeJSONString(raw)
}

func (h *Handle) EmbedAudio(pcm []float32) ([]float32, error) {
	result, err := decodeEmbeddingResult(h.EmbedAudioJSON(pcm))
	if err != nil {
		return nil, err
	}
	return result.Embedding, nil
}

func (h *Handle) ProcessImageJSON(data []byte, width int, height int, channels int, sourceID string) ([]byte, error) {
	return h.ProcessImageJSONWithOptions(data, width, height, channels, sourceID, nil)
}

func (h *Handle) ProcessImageJSONWithOptions(data []byte, width int, height int, channels int, sourceID string, options *ProcessOptions) ([]byte, error) {
	if err := h.require(); err != nil {
		return nil, err
	}
	if err := validateImageBuffer(data, width, height, channels); err != nil {
		return nil, err
	}
	nativeOptions := makeNativeProcessOptions(options)
	raw := fnProcessImageJSONOpts(
		h.ptr,
		bytePtr(data),
		int32(width),
		int32(height),
		int32(channels),
		sourceID,
		&nativeOptions,
	)
	runtime.KeepAlive(data)
	runtime.KeepAlive(h)
	return takeJSONString(raw)
}

func (h *Handle) ProcessImageWithMediaJSON(data []byte, width int, height int, channels int, sourceID string, media *Media) ([]byte, error) {
	return h.ProcessImageWithMediaJSONWithOptions(data, width, height, channels, sourceID, media, nil)
}

func (h *Handle) ProcessImageWithMediaJSONWithOptions(data []byte, width int, height int, channels int, sourceID string, media *Media, options *ProcessOptions) ([]byte, error) {
	if err := h.require(); err != nil {
		return nil, err
	}
	if err := validateImageBuffer(data, width, height, channels); err != nil {
		return nil, err
	}
	nativeMedia, keep := makeNativeMedia(media)
	nativeOptions := makeNativeProcessOptions(options)
	raw := fnProcessImageMediaOpts(
		h.ptr,
		bytePtr(data),
		int32(width),
		int32(height),
		int32(channels),
		sourceID,
		&nativeMedia,
		&nativeOptions,
	)
	runtime.KeepAlive(data)
	runtime.KeepAlive(keep)
	runtime.KeepAlive(h)
	return takeJSONString(raw)
}

func (h *Handle) ProcessImage(data []byte, width int, height int, channels int, sourceID string) (map[string]any, error) {
	return decodeJSONObject(h.ProcessImageJSON(data, width, height, channels, sourceID))
}

func (h *Handle) ProcessImageWithOptions(data []byte, width int, height int, channels int, sourceID string, options *ProcessOptions) (map[string]any, error) {
	return decodeJSONObject(h.ProcessImageJSONWithOptions(data, width, height, channels, sourceID, options))
}

func (h *Handle) ProcessImageWithMedia(data []byte, width int, height int, channels int, sourceID string, media *Media) (map[string]any, error) {
	return decodeJSONObject(h.ProcessImageWithMediaJSON(data, width, height, channels, sourceID, media))
}

func (h *Handle) ProcessImageWithMediaAndOptions(data []byte, width int, height int, channels int, sourceID string, media *Media, options *ProcessOptions) (map[string]any, error) {
	return decodeJSONObject(h.ProcessImageWithMediaJSONWithOptions(data, width, height, channels, sourceID, media, options))
}

func (h *Handle) EmbedImageJSON(data []byte, width int, height int, channels int) ([]byte, error) {
	if err := h.require(); err != nil {
		return nil, err
	}
	if err := validateImageBuffer(data, width, height, channels); err != nil {
		return nil, err
	}
	raw := fnEmbedImageJSON(h.ptr, bytePtr(data), int32(width), int32(height), int32(channels))
	runtime.KeepAlive(data)
	runtime.KeepAlive(h)
	return takeJSONString(raw)
}

func (h *Handle) EmbedImage(data []byte, width int, height int, channels int) ([]float32, error) {
	result, err := decodeEmbeddingResult(h.EmbedImageJSON(data, width, height, channels))
	if err != nil {
		return nil, err
	}
	return result.Embedding, nil
}

func (h *Handle) ConsolidateJSON() ([]byte, error) {
	if err := h.require(); err != nil {
		return nil, err
	}
	raw := fnConsolidateJSON(h.ptr)
	runtime.KeepAlive(h)
	return takeJSONString(raw)
}

func (h *Handle) Consolidate() (map[string]any, error) {
	return decodeJSONObject(h.ConsolidateJSON())
}

func (h *Handle) Flush() error {
	if err := h.require(); err != nil {
		return err
	}
	status := fnFlush(h.ptr)
	runtime.KeepAlive(h)
	if status != 0 {
		return lastError()
	}
	return nil
}

func (h *Handle) Reset() error {
	if err := h.require(); err != nil {
		return err
	}
	status := fnReset(h.ptr)
	runtime.KeepAlive(h)
	if status != 0 {
		return lastError()
	}
	return nil
}

func (h *Handle) require() error {
	if h == nil || h.ptr == 0 {
		return errors.New("cortext handle is closed")
	}
	return nil
}

func validateImageBuffer(data []byte, width int, height int, channels int) error {
	if width <= 0 || height <= 0 || channels <= 0 {
		return errors.New("width, height, and channels must be positive")
	}
	const maxCInt = int64(1<<31 - 1)
	if int64(width) > maxCInt || int64(height) > maxCInt || int64(channels) > maxCInt {
		return errors.New("image dimensions exceed the native int range")
	}
	maxInt := int(^uint(0) >> 1)
	if width > maxInt/height || width*height > maxInt/channels {
		return errors.New("image dimensions overflow addressable memory")
	}
	expected := width * height * channels
	if len(data) < expected {
		return errors.New("image buffer is smaller than width * height * channels")
	}
	return nil
}

// keepAliveMedia holds Go memory referenced by a nativeMedia for the call.
type keepAliveMedia struct {
	data []byte
	mime []byte
}

func makeNativeMedia(media *Media) (nativeMedia, *keepAliveMedia) {
	if media == nil || len(media.Data) == 0 {
		return nativeMedia{}, nil
	}
	keep := &keepAliveMedia{
		data: media.Data,
		mime: append([]byte(media.MimeType), 0),
	}
	native := nativeMedia{
		Data:     &keep.data[0],
		Size:     uintptr(len(keep.data)),
		MimeType: &keep.mime[0],
	}
	return native, keep
}

func makeNativeProcessOptions(options *ProcessOptions) nativeProcessOptions {
	native := nativeProcessOptions{}
	native.StructSize = unsafe.Sizeof(native)
	fnProcessJSONOptionsInit(&native)
	if options != nil {
		if options.OmitEmbedding {
			native.IncludeEmbedding = 0
		}
		if options.Retention != 0 {
			native.Retention = nativeRetention(options.Retention)
		}
	}
	return native
}

func nativeRetention(retention Retention) int32 {
	switch retention {
	case RetentionDurable:
		return retentionDurableC
	case RetentionBoundary:
		return retentionBoundaryC
	case RetentionEphemeral:
		return retentionEphemeralC
	default:
		return retentionNaturalC
	}
}

func lastError() error {
	msg := ""
	if fnLastError != nil {
		msg = fnLastError()
	}
	if msg == "" {
		return errors.New("cortext call failed")
	}
	return errors.New(msg)
}

func decodeJSONObject(payload []byte, err error) (map[string]any, error) {
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if unmarshalErr := json.Unmarshal(payload, &out); unmarshalErr != nil {
		return nil, unmarshalErr
	}
	return out, nil
}

func decodeEmbeddingResult(payload []byte, err error) (EmbeddingResult, error) {
	if err != nil {
		return EmbeddingResult{}, err
	}
	var out EmbeddingResult
	if unmarshalErr := json.Unmarshal(payload, &out); unmarshalErr != nil {
		return EmbeddingResult{}, unmarshalErr
	}
	return out, nil
}

// ensureModelEnv sets CORTEXT_AIST_MODEL_PATH when unset if a model is found.
// Returns a restore function that puts the previous env value back.
func ensureModelEnv(dbPath string) (func(), error) {
	if os.Getenv("CORTEXT_AIST_MODEL_PATH") != "" {
		return func() {}, nil
	}

	path, err := resolveAISTModel(dbPath)
	if err != nil {
		return nil, err
	}
	if path == "" {
		// Leave unset; native library may still resolve via relative models/.
		return func() {}, nil
	}
	if err := os.Setenv("CORTEXT_AIST_MODEL_PATH", path); err != nil {
		return nil, err
	}
	return func() {
		_ = os.Unsetenv("CORTEXT_AIST_MODEL_PATH")
	}, nil
}

// resolveAISTModel prefers bundled Git chunks, then a full local .gguf.
func resolveAISTModel(dbPath string) (string, error) {
	if chunksReady() {
		return MaterializeModel(dbPath)
	}
	return findDefaultAISTModel(), nil
}

func chunksReady() bool {
	manifestPath := filepath.Join(packageDir(), "models", "manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		return false
	}
	// At least one declared part must exist on disk.
	chunk0 := filepath.Join(packageDir(), "models", "AIST-87M-GGUF", "chunks", "AIST-87M_q8_0.gguf.part-000")
	_, err := os.Stat(chunk0)
	return err == nil
}

func findDefaultAISTModel() string {
	candidates := []string{}
	pkg := packageDir()
	if pkg != "" {
		candidates = append(candidates,
			filepath.Join(pkg, "models", "AIST-87M-GGUF", "AIST-87M_q8_0.gguf"),
			filepath.Join(pkg, "models", "AIST-87M-GGUF", "AIST-87M_q5_1.gguf"),
		)
		parent := filepath.Dir(pkg)
		candidates = append(candidates,
			filepath.Join(parent, "cortext", "models", "AIST-87M-GGUF", "AIST-87M_q8_0.gguf"),
			filepath.Join(parent, "cortext", "models", "AIST-87M-GGUF", "AIST-87M_q5_1.gguf"),
			filepath.Join(parent, "cortext", "bindings", "python", "augmem", "cortext", "models", "AIST-87M-GGUF", "AIST-87M_q8_0.gguf"),
		)
	}
	// CWD-relative search roots used by the native library itself.
	cwd, _ := os.Getwd()
	if cwd != "" {
		candidates = append(candidates,
			filepath.Join(cwd, "models", "AIST-87M-GGUF", "AIST-87M_q8_0.gguf"),
			filepath.Join(cwd, "models", "AIST-87M-GGUF", "AIST-87M_q5_1.gguf"),
		)
	}
	for _, path := range candidates {
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
