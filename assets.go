package cortext

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultAssetsRepo   = "augmem/cortext"
	assetsHTTPTimeout   = 10 * time.Minute
	assetsMarkerName    = ".cortext-assets-ok"
	releaseAssetNameTpl = "cortext-assets-%s.tar.gz"
)

var (
	assetsMu   sync.Mutex
	assetsRoot string
)

// EnsureAssets downloads (if needed) and unpacks the shared Cortext release
// asset tarball from github.com/augmem/cortext, then returns the root that
// contains native/ and models/.
//
// Layout after unpack:
//
//	native/<platform-tag>/libcortext.*
//	models/manifest.json
//	models/AIST-87M-GGUF/chunks/…
//	models/mdbr-leaf-ir/vocab.txt
//
// Resolution order for the asset root:
//  1. CORTEXT_ASSETS_DIR (pre-unpacked tree)
//  2. User/cache dir for AssetsVersion (download once, reuse)
//
// Override the release with CORTEXT_ASSETS_VERSION or the full archive URL with
// CORTEXT_ASSETS_URL. Checksum is verified from the release .sha256 asset
// unless CORTEXT_ASSETS_SHA256 is set.
func EnsureAssets() (string, error) {
	assetsMu.Lock()
	defer assetsMu.Unlock()
	if assetsRoot != "" {
		if err := validateAssetsRoot(assetsRoot); err == nil {
			return assetsRoot, nil
		}
		assetsRoot = ""
	}
	root, err := ensureAssetsLocked()
	if err != nil {
		return "", err
	}
	assetsRoot = root
	return assetsRoot, nil
}

// AssetsDir returns the last EnsureAssets root, or empty if not yet loaded.
func AssetsDir() string {
	assetsMu.Lock()
	defer assetsMu.Unlock()
	return assetsRoot
}

func ensureAssetsLocked() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("CORTEXT_ASSETS_DIR")); dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return "", err
		}
		if err := validateAssetsRoot(abs); err != nil {
			return "", fmt.Errorf("CORTEXT_ASSETS_DIR: %w", err)
		}
		return abs, nil
	}

	version := assetsVersion()
	root, err := defaultAssetsCacheRoot(version)
	if err != nil {
		return "", err
	}
	marker := filepath.Join(root, assetsMarkerName)
	if st, err := os.Stat(marker); err == nil && !st.IsDir() {
		if err := validateAssetsRoot(root); err == nil {
			return root, nil
		}
		// Incomplete or corrupt cache; rebuild.
		_ = os.RemoveAll(root)
	}

	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}

	url := assetsArchiveURL(version)
	if override := strings.TrimSpace(os.Getenv("CORTEXT_ASSETS_URL")); override != "" {
		url = override
	}

	wantSHA, err := resolveArchiveSHA256(version, url)
	if err != nil {
		return "", err
	}

	tmpRoot, err := os.MkdirTemp(filepath.Dir(root), ".cortext-assets-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpRoot)

	archivePath := filepath.Join(tmpRoot, fmt.Sprintf(releaseAssetNameTpl, version))
	if err := downloadFile(url, archivePath); err != nil {
		return "", fmt.Errorf("download cortext assets %s: %w", url, err)
	}
	got, err := fileSHA256(archivePath)
	if err != nil {
		return "", err
	}
	if wantSHA != "" && !strings.EqualFold(got, wantSHA) {
		return "", fmt.Errorf("cortext assets checksum mismatch: got %s want %s", got, wantSHA)
	}

	extractRoot := filepath.Join(tmpRoot, "extract")
	if err := os.MkdirAll(extractRoot, 0o755); err != nil {
		return "", err
	}
	if err := extractTarGz(archivePath, extractRoot); err != nil {
		return "", fmt.Errorf("extract cortext assets: %w", err)
	}
	if err := validateAssetsRoot(extractRoot); err != nil {
		return "", err
	}

	// Atomic-ish replace of the versioned cache directory.
	_ = os.RemoveAll(root)
	if err := os.Rename(extractRoot, root); err != nil {
		// Cross-device fallback.
		if copyErr := copyDir(extractRoot, root); copyErr != nil {
			return "", fmt.Errorf("install assets cache: %v / %w", err, copyErr)
		}
	}
	if err := os.WriteFile(filepath.Join(root, assetsMarkerName), []byte(version+"\n"), 0o644); err != nil {
		return "", err
	}
	return root, nil
}

func assetsVersion() string {
	if v := strings.TrimSpace(os.Getenv("CORTEXT_ASSETS_VERSION")); v != "" {
		return strings.TrimPrefix(v, "v")
	}
	return AssetsVersion
}

func assetsArchiveURL(version string) string {
	name := fmt.Sprintf(releaseAssetNameTpl, version)
	return fmt.Sprintf("https://github.com/%s/releases/download/v%s/%s", defaultAssetsRepo, version, name)
}

func assetsChecksumURL(version string) string {
	return assetsArchiveURL(version) + ".sha256"
}

func resolveArchiveSHA256(version, archiveURL string) (string, error) {
	if v := strings.TrimSpace(os.Getenv("CORTEXT_ASSETS_SHA256")); v != "" {
		return strings.ToLower(strings.TrimSpace(v)), nil
	}
	// Prefer the release .sha256 sidecar when available.
	sumURL := archiveURL + ".sha256"
	if archiveURL == assetsArchiveURL(version) {
		sumURL = assetsChecksumURL(version)
	}
	body, err := httpGetBytes(sumURL)
	if err != nil {
		// Checksum file optional only if caller set no expectation; still fail closed
		// for official release URLs so we do not install unauthenticated blobs.
		return "", fmt.Errorf("fetch assets checksum %s: %w", sumURL, err)
	}
	line := strings.TrimSpace(string(body))
	if line == "" {
		return "", fmt.Errorf("empty assets checksum from %s", sumURL)
	}
	// Formats: "<sha256>  filename" or bare hex.
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", fmt.Errorf("invalid assets checksum from %s", sumURL)
	}
	sum := strings.ToLower(fields[0])
	if len(sum) != 64 {
		return "", fmt.Errorf("invalid assets sha256 length from %s: %q", sumURL, sum)
	}
	return sum, nil
}

func defaultAssetsCacheRoot(version string) (string, error) {
	if dir := strings.TrimSpace(os.Getenv("CORTEXT_ASSET_CACHE_DIR")); dir != "" {
		return filepath.Abs(filepath.Join(dir, version))
	}
	base, err := defaultUserModelCache() // .../augmem/cortext/models parent pattern
	if err != nil {
		return "", err
	}
	// Reuse the same org cache root: .../augmem/cortext/assets/<version>
	root := filepath.Join(filepath.Dir(base), "assets", version)
	return root, nil
}

func validateAssetsRoot(root string) error {
	nativeManifest := filepath.Join(root, "native", "manifest.json")
	if st, err := os.Stat(nativeManifest); err != nil || st.IsDir() {
		return fmt.Errorf("missing native/manifest.json under %s", root)
	}
	tag := platformTag()
	lib := filepath.Join(root, "native", tag, libraryNameForTag(tag))
	if st, err := os.Stat(lib); err != nil || st.IsDir() {
		return fmt.Errorf("missing native library for %s at %s", tag, lib)
	}
	// models/ is optional for embed-on natives, but the release tarball always
	// includes it; require it so MaterializeModel can fall back when needed.
	modelManifest := filepath.Join(root, "models", "manifest.json")
	if st, err := os.Stat(modelManifest); err != nil || st.IsDir() {
		return fmt.Errorf("missing models/manifest.json under %s", root)
	}
	return nil
}

func nativeLibraryFromAssets(root string) string {
	tag := platformTag()
	return filepath.Join(root, "native", tag, libraryNameForTag(tag))
}

func modelsRootFromAssets(root string) string {
	return filepath.Join(root, "models")
}

func downloadFile(url, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".partial"
	_ = os.Remove(tmp)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "cortext.go/"+ModuleVersion)
	client := &http.Client{Timeout: assetsHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}

func httpGetBytes(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "cortext.go/"+ModuleVersion)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

func fileSHA256(path string) (string, error) {
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

func extractTarGz(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		// Normalize and reject path escape.
		name := filepath.Clean(hdr.Name)
		if name == "." || name == ".." || strings.HasPrefix(name, ".."+string(os.PathSeparator)) {
			continue
		}
		target := filepath.Join(destAbs, name)
		if !strings.HasPrefix(target, destAbs+string(os.PathSeparator)) && target != destAbs {
			return fmt.Errorf("tar entry escapes destination: %s", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := hdr.FileInfo().Mode()
			if mode == 0 {
				mode = 0o644
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		default:
			// Skip symlinks and other types for safety.
			continue
		}
	}
	return nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyPath(path, target)
	})
}

// ResetAssetsForTest clears the process-wide assets singleton (tests only).
func ResetAssetsForTest() {
	assetsMu.Lock()
	defer assetsMu.Unlock()
	assetsRoot = ""
}
