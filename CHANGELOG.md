# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/augmem/cortext.go/compare/v1.2.2...HEAD
[1.2.2]: https://github.com/augmem/cortext.go/releases/tag/v1.2.2
[1.2.1]: https://github.com/augmem/cortext.go/releases/tag/v1.2.1
[1.2.0]: https://github.com/augmem/cortext.go/compare/v1.2.0...v1.2.1
