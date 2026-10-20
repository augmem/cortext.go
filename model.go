package cortext

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

const (
	modelAssetName   = "AIST-87M-GGUF/AIST-87M_q8_0.gguf"
	vocabAssetName   = "mdbr-leaf-ir/vocab.txt"
	liModelAssetName = "AIST-87M-GGUF/AIST-87M-LI.gguf"
)

type modelManifest struct {
	Schema  string       `json:"schema"`
	Assets  []modelAsset `json:"assets"`
	Version string       `json:"cortext_version"`
}

type modelAsset struct {
	Filename string       `json:"filename"`
	SHA256   string       `json:"sha256"`
	Size     int64        `json:"size,omitempty"`
	Chunks   []modelChunk `json:"chunks,omitempty"`
}

type modelChunk struct {
	Filename string `json:"filename"`
	SHA256   string `json:"sha256"`
}

var (
	materializeMu    sync.Mutex
	materializedOnce sync.Map // cacheDir -> model path
)

// MaterializeModel reassembles AIST model chunks from the release asset tree
// into a local cache and returns the full .gguf path. When the manifest
// declares the late-interaction reranker (AIST-87M-LI.gguf, required by v5
// engines), it is checksummed and placed beside the AIST file, where the
// engine looks for it unless CORTEXT_LI_MODEL_PATH is set.
//
// Release natives embed AIST and assemble it inside the library on first create.
// Call MaterializeModel only when an external filesystem GGUF is required.
//
// Chunks always come from EnsureAssets() (augmem/cortext.cpp release tarball).
//
// Cache location (first match wins):
//  1. CORTEXT_MODEL_CACHE_DIR
//  2. sibling of dbPath: <dir(dbPath)>/.cortext-assets  (":memory:" → cwd)
//  3. user cache under augmem/cortext/models
func MaterializeModel(dbPath string) (string, error) {
	cacheDir, err := modelCacheDir(dbPath)
	if err != nil {
		return "", err
	}
	if path, ok := cachedMaterialized(cacheDir); ok {
		return path, nil
	}

	materializeMu.Lock()
	defer materializeMu.Unlock()

	if path, ok := cachedMaterialized(cacheDir); ok {
		return path, nil
	}

	root, err := EnsureAssets()
	if err != nil {
		return "", err
	}
	modelsRoot := modelsRootFromAssets(root)

	path, err := materializeModelLocked(cacheDir, modelsRoot)
	if err != nil {
		return "", err
	}
	materializedOnce.Store(cacheDir, path)
	return path, nil
}

func cachedMaterialized(cacheDir string) (string, bool) {
	v, ok := materializedOnce.Load(cacheDir)
	if !ok {
		return "", false
	}
	path, ok := v.(string)
	if !ok || path == "" {
		return "", false
	}
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		materializedOnce.Delete(cacheDir)
		return "", false
	}
	return path, true
}

func materializeModelLocked(cacheDir, modelsRoot string) (string, error) {
	manifest, err := loadModelManifest(modelsRoot)
	if err != nil {
		return "", err
	}
	asset, err := findModelAsset(manifest, modelAssetName)
	if err != nil {
		return "", err
	}
	if err := ensureVocabInCache(cacheDir, modelsRoot, manifest); err != nil {
		return "", err
	}
	modelPath, err := materializeAsset(cacheDir, modelsRoot, asset)
	if err != nil {
		return "", err
	}
	// Engines from v5 on require the late-interaction reranker whenever AIST
	// resolves, and look for it beside the AIST file. Manifests that predate
	// it (and their engines) do not declare it.
	if li, err := findModelAsset(manifest, liModelAssetName); err == nil {
		if _, err := materializeAsset(cacheDir, modelsRoot, li); err != nil {
			return "", err
		}
	}
	return modelPath, nil
}

// materializeAsset installs one manifest asset under cacheDir and returns its
// path. The source is the assembled file in modelsRoot when its SHA-256
// matches, otherwise its declared chunks, each checksummed and reassembled.
func materializeAsset(cacheDir, modelsRoot string, asset *modelAsset) (string, error) {
	dest := filepath.Join(cacheDir, filepath.FromSlash(asset.Filename))
	if ok, _ := fileHasSHA256(dest, asset.SHA256); ok {
		return dest, nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	assembled := filepath.Join(modelsRoot, filepath.FromSlash(asset.Filename))
	useAssembled, _ := fileHasSHA256(assembled, asset.SHA256)
	if !useAssembled && len(asset.Chunks) == 0 {
		return "", fmt.Errorf("cortext model asset %s is missing and has no chunks declared", asset.Filename)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dest), ".cortext-model-*.tmp")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()

	hasher := sha256.New()
	writer := io.MultiWriter(tmp, hasher)
	if useAssembled {
		if err := copyFile(writer, assembled); err != nil {
			return "", fmt.Errorf("read model asset %s: %w", asset.Filename, err)
		}
	} else {
		for _, chunk := range asset.Chunks {
			chunkPath := filepath.Join(modelsRoot, filepath.FromSlash(chunk.Filename))
			if ok, actual := fileHasSHA256(chunkPath, chunk.SHA256); !ok {
				if actual == "" {
					return "", fmt.Errorf("cortext model chunk missing: %s", chunk.Filename)
				}
				return "", fmt.Errorf("cortext model chunk checksum mismatch: %s (got %s)", chunk.Filename, actual)
			}
			if err := copyFile(writer, chunkPath); err != nil {
				return "", fmt.Errorf("read model chunk %s: %w", chunk.Filename, err)
			}
		}
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	actual := hex.EncodeToString(hasher.Sum(nil))
	if actual != asset.SHA256 {
		return "", fmt.Errorf("cortext model %s checksum mismatch: got %s want %s", asset.Filename, actual, asset.SHA256)
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		if copyErr := copyPath(tmpPath, dest); copyErr != nil {
			return "", fmt.Errorf("install model %s: %v / %w", asset.Filename, err, copyErr)
		}
		_ = os.Remove(tmpPath)
	}
	return dest, nil
}

func ensureVocabInCache(cacheDir, modelsRoot string, manifest *modelManifest) error {
	asset, err := findModelAsset(manifest, vocabAssetName)
	if err != nil {
		return err
	}
	dest := filepath.Join(cacheDir, filepath.FromSlash(vocabAssetName))
	if ok, _ := fileHasSHA256(dest, asset.SHA256); ok {
		return nil
	}
	src := filepath.Join(modelsRoot, filepath.FromSlash(vocabAssetName))
	if ok, actual := fileHasSHA256(src, asset.SHA256); !ok {
		if actual == "" {
			return fmt.Errorf("cortext vocab asset missing: %s", vocabAssetName)
		}
		return fmt.Errorf("cortext vocab checksum mismatch: %s", vocabAssetName)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return copyPath(src, dest)
}

func loadModelManifest(modelsRoot string) (*modelManifest, error) {
	path := filepath.Join(modelsRoot, "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cortext model manifest unreadable: %w", err)
	}
	var manifest modelManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("cortext model manifest invalid: %w", err)
	}
	return &manifest, nil
}

func findModelAsset(manifest *modelManifest, filename string) (*modelAsset, error) {
	for i := range manifest.Assets {
		if manifest.Assets[i].Filename == filename {
			return &manifest.Assets[i], nil
		}
	}
	return nil, fmt.Errorf("cortext model asset not declared: %s", filename)
}

func modelCacheDir(dbPath string) (string, error) {
	if env := os.Getenv("CORTEXT_MODEL_CACHE_DIR"); env != "" {
		return filepath.Abs(env)
	}
	if dbPath != "" && dbPath != ":memory:" {
		abs, err := filepath.Abs(dbPath)
		if err == nil {
			return filepath.Join(filepath.Dir(abs), ".cortext-assets"), nil
		}
	}
	cwd, err := os.Getwd()
	if err == nil {
		return filepath.Join(cwd, ".cortext-assets"), nil
	}
	return defaultUserModelCache()
}

func defaultUserModelCache() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Caches", "augmem", "cortext", "models"), nil
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, "augmem", "cortext", "models"), nil
	default:
		base := os.Getenv("XDG_CACHE_HOME")
		if base == "" {
			base = filepath.Join(home, ".cache")
		}
		return filepath.Join(base, "augmem", "cortext", "models"), nil
	}
}

func fileHasSHA256(path, want string) (bool, string) {
	f, err := os.Open(path)
	if err != nil {
		return false, ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, ""
	}
	got := hex.EncodeToString(h.Sum(nil))
	return got == want, got
}

func copyFile(w io.Writer, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

func copyPath(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
