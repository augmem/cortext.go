package cortext

import (
	"fmt"
	"unsafe"

	"github.com/ebitengine/purego"
)

// C ABI structs matching include/cortext/capi.h (64-bit).

type nativeConfig struct {
	StructSize               uintptr
	Focus                    float64
	Sensitivity              float64
	Stability                float64
	AffectInterrupt          int32
	AffectRetrieval          int32
	ReinforcementEnabled     int32
	SequentialEdgesEnabled   int32
	SignalFilterAudioEnabled int32
	SignalFilterImageEnabled int32
	SignalFilterTextEnabled  int32
}

type nativeMedia struct {
	Data     *byte
	Size     uintptr
	MimeType *byte
}

type nativeProcessOptions struct {
	StructSize       uintptr
	IncludeEmbedding int32
	Retention        int32
	Reserved         int32
	// EvidenceBudgetBytes: 0 = unset (whole memories).
	EvidenceBudgetBytes uintptr
}

const (
	retentionNaturalC   int32 = 0
	retentionDurableC   int32 = 1
	retentionBoundaryC  int32 = 2
	retentionEphemeralC int32 = 3
)

// Registered C ABI entry points (purego, no CGO).
var (
	fnConfigInit              func(*nativeConfig)
	fnCreateWithConfig        func(*nativeConfig, string) uintptr
	fnFree                    func(uintptr)
	fnProcessTextJSONOpts     func(uintptr, string, string, *nativeProcessOptions) uintptr
	fnEmbedTextJSON           func(uintptr, string) uintptr
	fnProcessAudioJSONOpts    func(uintptr, *float32, uintptr, string, *nativeProcessOptions) uintptr
	fnProcessAudioMediaOpts   func(uintptr, *float32, uintptr, string, *nativeMedia, *nativeProcessOptions) uintptr
	fnEmbedAudioJSON          func(uintptr, *float32, uintptr) uintptr
	fnProcessImageJSONOpts    func(uintptr, *byte, int32, int32, int32, string, *nativeProcessOptions) uintptr
	fnProcessImageMediaOpts   func(uintptr, *byte, int32, int32, int32, string, *nativeMedia, *nativeProcessOptions) uintptr
	fnEmbedImageJSON          func(uintptr, *byte, int32, int32, int32) uintptr
	fnConsolidateJSON         func(uintptr) uintptr
	fnFlush                   func(uintptr) int32
	fnReset                   func(uintptr) int32
	fnVersion                 func() string
	fnLastError               func() string
	fnStringFree              func(uintptr)
	fnProcessJSONOptionsInit  func(*nativeProcessOptions)
)

func registerFuncs(handle uintptr) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("register cortext symbols: %v", r)
		}
	}()

	purego.RegisterLibFunc(&fnConfigInit, handle, "cortext_config_init")
	purego.RegisterLibFunc(&fnCreateWithConfig, handle, "cortext_create_with_config")
	purego.RegisterLibFunc(&fnFree, handle, "cortext_free")
	purego.RegisterLibFunc(&fnProcessTextJSONOpts, handle, "cortext_process_text_json_with_options")
	purego.RegisterLibFunc(&fnEmbedTextJSON, handle, "cortext_embed_text_json")
	purego.RegisterLibFunc(&fnProcessAudioJSONOpts, handle, "cortext_process_audio_json_with_options")
	purego.RegisterLibFunc(&fnProcessAudioMediaOpts, handle, "cortext_process_audio_with_media_json_with_options")
	purego.RegisterLibFunc(&fnEmbedAudioJSON, handle, "cortext_embed_audio_json")
	purego.RegisterLibFunc(&fnProcessImageJSONOpts, handle, "cortext_process_image_json_with_options")
	purego.RegisterLibFunc(&fnProcessImageMediaOpts, handle, "cortext_process_image_with_media_json_with_options")
	purego.RegisterLibFunc(&fnEmbedImageJSON, handle, "cortext_embed_image_json")
	purego.RegisterLibFunc(&fnConsolidateJSON, handle, "cortext_consolidate_json")
	purego.RegisterLibFunc(&fnFlush, handle, "cortext_flush")
	purego.RegisterLibFunc(&fnReset, handle, "cortext_reset")
	purego.RegisterLibFunc(&fnVersion, handle, "cortext_version")
	purego.RegisterLibFunc(&fnLastError, handle, "cortext_last_error")
	purego.RegisterLibFunc(&fnStringFree, handle, "cortext_string_free")
	purego.RegisterLibFunc(&fnProcessJSONOptionsInit, handle, "cortext_process_json_options_init")
	return nil
}

func goStringFromC(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	// Walk null-terminated C string without depending on purego internals.
	var n int
	for {
		if *(*byte)(unsafe.Pointer(ptr + uintptr(n))) == 0 {
			break
		}
		n++
		if n > 1<<20 {
			break
		}
	}
	if n == 0 {
		return ""
	}
	b := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), n)
	return string(b)
}

func takeJSONString(ptr uintptr) ([]byte, error) {
	if ptr == 0 {
		return nil, lastError()
	}
	defer fnStringFree(ptr)
	return []byte(goStringFromC(ptr)), nil
}

func boolToCInt(value bool) int32 {
	if value {
		return 1
	}
	return 0
}

func float32Ptr(pcm []float32) *float32 {
	if len(pcm) == 0 {
		return nil
	}
	return &pcm[0]
}

func bytePtr(data []byte) *byte {
	if len(data) == 0 {
		return nil
	}
	return &data[0]
}

func cString(s string) (*byte, []byte) {
	buf := append([]byte(s), 0)
	return &buf[0], buf
}
