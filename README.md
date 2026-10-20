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

	// Attach arbitrary binary source media without changing the text signal.
	withMedia, err := engine.ProcessTextWithMedia(
		"Bailey sent a voice note.", "chat/main",
		&cortext.Media{Data: []byte{0x00, 0xff, 0x01}, MimeType: "audio/ogg"},
	)
	if err != nil {
		panic(err)
	}
	fmt.Println(withMedia["should_interrupt"])

	embedding, err := engine.EmbedText("embed without storing")
	if err != nil {
		panic(err)
	}
	fmt.Println(len(embedding))
}
```

A query context leads with `standing_memory` (standing directives such as
"Always ...", outside the retrieved ranking; show it ahead of
`retrieved_memory` and do not count it against a cut of the retrieved list),
then `retrieved_memory`. `ProcessOptions.EvidenceBudgetBytes` (zero = whole
memories) fits both within a byte budget: text counts its UTF-8 bytes, other
payloads 480 bytes each, grouped memories keep their members most similar to
the query, and more distinct memories are kept trimmed before lower-ranked
ones are left out (8,000 tokens at 4.35 chars/token is 34,800 bytes).

```go
ctx, err := engine.ProcessTextWithOptions("When is the marathon long run?", "query/main",
	&cortext.ProcessOptions{Retention: cortext.RetentionEphemeral, EvidenceBudgetBytes: 34_800})
```

Each memory in `standing_memory` / `retrieved_memory` / `working_memory` carries `content`: every
signal payload of the memory as `{size_bytes, mimetype, base64}`, in signal
order. `modality` is the modality its signals share, or `"mixed"` when they
differ (for example a turn's text and the photo sent with it under one source).

`ProcessText` remains available for text-only input. To retain source media
alongside text, use `ProcessTextWithMedia` (or its `JSON`/
`WithMediaAndOptions` variants). `Media.Data` is passed as arbitrary binary and
`Media.MimeType` is preserved as supplied.

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
