# cortext.go

Pure Go package for [Cortext](https://github.com/augmem/cortext) — no CGO required.

Natives and AIST model shards are **not** in this module. On first use the package
downloads the matching [augmem/cortext.cpp](https://github.com/augmem/cortext.cpp/releases)
release asset, verifies its SHA-256, and unpacks it:

```text
cortext-assets-<version>.tar.gz
cortext-assets-<version>.tar.gz.sha256
```

Default assets version: **1.2.4** (`AssetsVersion`).

## Install

```bash
go get github.com/augmem/cortext.go@v1.2.4
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

## Assets

| Env | Purpose |
| --- | --- |
| `CORTEXT_ASSETS_VERSION` | Release version to fetch (default `1.2.4`) |
| `CORTEXT_ASSETS_DIR` | Pre-unpacked release tree (`native/` + `models/`) — skips download |
| `CORTEXT_ASSETS_URL` | Override archive URL |
| `CORTEXT_ASSETS_SHA256` | Override expected archive checksum |
| `CORTEXT_ASSET_CACHE_DIR` | Parent cache dir for downloaded versions |
| `CORTEXT_LIBRARY_PATH` | Explicit shared library path (skips asset native selection) |
| `CORTEXT_AIST_MODEL_PATH` | Explicit full `.gguf` only when you need one on disk |

There is no vendored `native/` or `models/` tree and no sibling-checkout search.
Load order:

1. `CORTEXT_LIBRARY_PATH` if set
2. Otherwise `EnsureAssets()` → `native/<platform-tag>/…` from the release tarball

Release natives embed AIST and assemble it on first create. `MaterializeModel`
is optional and always reads chunks from the same release asset tree.

Offline prep:

```bash
./scripts/fetch_assets.sh /path/to/assets
export CORTEXT_ASSETS_DIR=/path/to/assets
```

## Develop

```bash
CGO_ENABLED=0 go test . -timeout 20m
CGO_ENABLED=0 go test . -short   # no network
```

## Release

```bash
git tag -a v1.2.4 -m "cortext.go v1.2.4"
git push origin v1.2.4
```

Bump `AssetsVersion` in `version.go` when pinning a new augmem/cortext.cpp assets line.
