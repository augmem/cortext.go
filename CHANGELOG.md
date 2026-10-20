# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `ProcessOptions.EvidenceBudgetBytes` and the `evidence_budget_bytes` field of
  the native `cortext_process_json_options` struct: a per-call evidence budget
  (zero returns recalled memories whole). Requires an engine with the field.
- README: query contexts lead with `standing_memory`, ahead of
  `retrieved_memory`.
- Text processing with arbitrary source media payloads and MIME types through
  `ProcessTextWithMedia` and JSON/options variants.

## [1.2.4] - 2026-07-24

### Changed

- **Hard cutover** to augmem/cortext release assets only (`EnsureAssets`)
- Download + SHA-256 verify `cortext-assets-1.2.4.tar.gz` on first use
- Load only `CORTEXT_LIBRARY_PATH` or the release-asset native for the host tag
- Rely on embed-on natives for AIST (no auto model-path search or materialize)
- `MaterializeModel` still available, and always reads shards from release assets

### Removed

- Vendored `native/` and `models/` trees from the module repository
- Sibling cortext checkout / ffi-release / zig-out library search
- Package-local and CWD model discovery
- `scripts/build_native.py` and `scripts/shard_model.py`

## [1.2.2] - 2026-07-23

### Fixed

- Windows CI: binary `.gitattributes` for model/native assets (no CRLF rewrite)
- Windows runtime: set `CORTEXT_AIST_MODEL_PATH` via CRT `_putenv_s` so the native library's `getenv` sees it
- Drop memoized materialize paths when the reassembled model file no longer exists

## [1.2.1] - 2026-07-23

### Fixed

- Windows library loading via `syscall.LoadLibrary` (purego has no `Dlopen` on Windows)

## [1.2.0] - 2026-07-23

### Added

- Pure-Go (no CGO) package over the Cortext C ABI via purego
- Bundled Zig cross-built shared libraries for linux/macOS/Windows (x86_64 + aarch64)
- Git-chunked AIST-87M model + vocab with local reassembly and SHA-256 verification
- Public API aligned with Cortext bindings: process/embed/consolidate/flush/reset
- `scripts/build_native.py` and `scripts/shard_model.py` for refreshing artifacts
- GitHub Actions CI and GoReleaser release workflow

[Unreleased]: https://github.com/augmem/cortext.go/compare/v1.2.4...HEAD
[1.2.4]: https://github.com/augmem/cortext.go/releases/tag/v1.2.4
[1.2.2]: https://github.com/augmem/cortext.go/compare/v1.2.2...v1.2.4
[1.2.1]: https://github.com/augmem/cortext.go/releases/tag/v1.2.1
[1.2.0]: https://github.com/augmem/cortext.go/compare/v1.2.0...v1.2.1
