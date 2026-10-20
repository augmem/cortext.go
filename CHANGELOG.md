# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Requires the next augmem/cortext.cpp release (engine v5, cortext.cpp `main`
f0b681e9 or later). The pinned `AssetsVersion` is unchanged, so the default
native does not provide these features yet; set `CORTEXT_LIBRARY_PATH` to a
newer build.

### Breaking

- `Config.ProceduralEnabled` removed, along with `procedural_enabled` in the
  native `cortext_config` struct (C layout change).
- Soft Anchors removed from the engine: their config, stats, memory fields and
  JSON keys are gone from the context packet.
- Memories are no longer trimmed to 3 KiB of content; they come back whole
  unless `ProcessOptions.EvidenceBudgetBytes` is set.
- v5 engines require the late-interaction reranker `AIST-87M-LI.gguf` whenever
  an AIST model resolves; `New` fails without it.

### Added

- `ProcessOptions.EvidenceBudgetBytes` (native `evidence_budget_bytes`): a
  per-call evidence budget for every process call; zero returns memories whole.
- `ProcessOptions.ReferenceTime` and `ProcessOptions.AsOf` (native
  `has_reference_ms` / `as_of` / `reference_ms`): reference-time retrieval for
  `RetentionEphemeral` queries, optionally hiding memories stored after it.
- `ProcessTextWithMedia`, `ProcessTextWithMediaAndOptions` and their `JSON`
  variants: store a caller payload under its MIME type in place of the text.
  Empty media stores the text; `text/*` payloads are used as text. Natives
  without the entry point return an error. Folds in #1.
- Context packets carry `standing_memory` ahead of `retrieved_memory`, a
  per-payload `timestamp` (ms, 0 = unknown) on every `content[]` entry,
  per-payload `mimetype`, and `modality: "mixed"` for memories whose signals
  differ (README documents the shape).
- `MaterializeModel` checksums and places `AIST-87M-LI.gguf` beside the AIST
  model when the manifest declares it, and can install either model from an
  already-assembled file in the models tree instead of its chunks.
- `CORTEXT_LI_MODEL_PATH` is validated by `New` and forwarded to the native
  (through the CRT on Windows), like `CORTEXT_AIST_MODEL_PATH`.

### Changed

- Options the loaded native cannot read (an evidence budget or reference time
  on an older engine) return an error instead of being silently ignored.
- Process options are offered to the native in each known layout, newest
  first, so a pre-v5 native (which only keeps its exact struct size) still
  reads retention.
- On a pre-v5 native, `New` rejects `Config.SequentialEdgesEnabled` and
  `Config.SignalFilter*`, which that engine would read one field off because
  its `cortext_config` still carries `procedural_enabled`.

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
