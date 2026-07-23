# cortext.go

Pure Go package for [Cortext](https://github.com/augmem/cortext) — no CGO required.

The package loads a prebuilt shared library from `native/<platform>/` (Zig
cross-compiled from the Cortext tree) at runtime via
[`purego`](https://github.com/ebitengine/purego). Consumers can `go get` this
module and cross-compile with `CGO_ENABLED=0`.

## Install

```bash
go get github.com/augmem/cortext.go
```

## Use

```go
package main

import (
	"fmt"

	cortext "github.com/augmem/cortext.go"
)

func main() {
	engine, err := cortext.New(":memory:", nil)
	if err != nil {
		panic(err)
	}
	defer engine.Close()

	ctx, err := engine.ProcessText("Bailey likes tennis balls.", "chat/main")
	if err != nil {
		panic(err)
	}
	fmt.Println(ctx["should_interrupt"])

	embedding, err := engine.EmbedText("embed without storing")
	if err != nil {
		panic(err)
	}
	fmt.Println(len(embedding))
}
```

Optional config (nil fields keep native defaults):

```go
cfg := &cortext.Config{
	Focus:           cortext.Ptr(0.6),
	AffectInterrupt: cortext.Ptr(true),
}
engine, err := cortext.New("memory.db", cfg)
```

## Native libraries

Bundled platforms (see `native/manifest.json`):

| Tag | Artifact |
| --- | --- |
| `linux-x86_64` | `libcortext.so` |
| `linux-aarch64` | `libcortext.so` |
| `macos-x86_64` | `libcortext.dylib` |
| `macos-aarch64` | `libcortext.dylib` |
| `windows-x86_64` | `cortext.dll` |
| `windows-aarch64` | `cortext.dll` |

Load order:

1. `CORTEXT_LIBRARY_PATH` (explicit shared library path)
2. `native/<platform-tag>/` inside this module
3. Sibling development trees (`../cortext/build/ffi-release`, `../cortext/zig-out/lib`, …)

Rebuild / refresh natives from a Cortext checkout (default `../cortext`):

```bash
# Zig cross-build every platform into native/
python3 scripts/build_native.py

# One platform only
python3 scripts/build_native.py --target macos-aarch64

# Copy already-built Python package natives
python3 scripts/build_native.py --from-python-natives
```

## Models

Cortext requires the AIST GGUF encoder. The ~135 MiB `AIST-87M_q8_0.gguf` is
**sharded into Git-friendly chunks** under `models/AIST-87M-GGUF/chunks/` (same
layout as [cortext-hermes-plugin](https://github.com/augmem/cortext-hermes-plugin)):
parts stay under 100 MiB so ordinary Git works without LFS.

On first `New`, the package:

1. Verifies each chunk’s SHA-256 from `models/manifest.json`
2. Concatenates them into a local cache
3. Verifies the reassembled model SHA-256
4. Copies `models/mdbr-leaf-ir/vocab.txt` beside it
5. Sets `CORTEXT_AIST_MODEL_PATH` for the native create call

Cache location (first match):

1. `CORTEXT_MODEL_CACHE_DIR`
2. `<dir(dbPath)>/.cortext-assets` (for `:memory:`, the process cwd)
3. OS user cache under `augmem/cortext/models`

Override with a full model path anytime:

```bash
export CORTEXT_AIST_MODEL_PATH=/path/to/AIST-87M_q8_0.gguf
```

Refresh shards from a full GGUF (or adopt existing hermes/plugin chunks):

```bash
python3 scripts/shard_model.py
python3 scripts/shard_model.py --source ../cortext/models/AIST-87M-GGUF/AIST-87M_q8_0.gguf
```

## API

Matches the Cortext C ABI / other language bindings:

- `Config`, `Ptr`, `Media`, `Retention`, `ProcessOptions`
- `New`, `Close`, `Version`, `LastError`, `LoadLibrary`
- `ProcessText` / `ProcessAudio` / `ProcessImage` (+ JSON / options / media variants)
- `EmbedText` / `EmbedAudio` / `EmbedImage`
- `Consolidate`, `Flush`, `Reset`

Audio input is 16 kHz mono float32 PCM. Image input is row-major RGB/RGBA with
explicit dimensions.

## Develop

```bash
# pure Go, no toolchain CGO; materializes model from checked-in chunks
CGO_ENABLED=0 go test .

CGO_ENABLED=0 go test . -run 'TestMaterialize|TestNewProcessEmbed' -v
```

## Relation to augmem/cortext

Source of truth for the engine and C ABI is [augmem/cortext](https://github.com/augmem/cortext).
This repository is the distributable Go package: purego bindings plus shipped
Zig-built shared libraries so application code never needs CGO or a local C++
build.

## Release

Tags follow semver and are intended to track the bundled Cortext C ABI major
line (for example `v1.2.0` ships Cortext 1.2.x natives).

```bash
# CI runs tests on push/PR (ubuntu, macOS, Windows; CGO_ENABLED=0)
# Cutting a release:
git tag -a v1.2.0 -m "cortext.go v1.2.0"
git push origin v1.2.0
```

Pushing a `v*` tag runs [GoReleaser](https://goreleaser.com/)
(`.github/workflows/release.yml`), which:

1. Runs `go test` with CGO disabled
2. Builds source + natives/model archives
3. Publishes a GitHub Release with checksums and changelog

Manual snapshot (no publish): Actions → Release → Run workflow.

After the first tag is public, module consumers can:

```bash
go get github.com/augmem/cortext.go@v1.2.0
```
