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
	jpegBytes := []byte{0xff, 0xd8, 0xff, 0xd9}
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

	// Store a caller payload (here a photo) in place of the text; the text is
	// still what the signal is encoded from.
	withMedia, err := engine.ProcessTextWithMedia(
		"A photo of the parking ticket.", "chat/main",
		&cortext.Media{Data: jpegBytes, MimeType: "image/jpeg"},
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

> The features below need an engine built from augmem/cortext.cpp `main`
> (engine v5 or later). No engine release contains them yet, so the pinned
> assets version still loads an older native; point `CORTEXT_LIBRARY_PATH` at a
> newer build to use them.

### Context packet

A query context leads with `standing_memory` (standing directives such as
"Always ...", outside the retrieved ranking; show it ahead of
`retrieved_memory` and do not count it against a cut of the retrieved list),
then `retrieved_memory`, then `working_memory`.

Each memory carries `content`: every signal payload of the memory as
`{size_bytes, mimetype, timestamp, base64}`, in signal order. `timestamp` is
that payload's stored signal time in ms since the Unix epoch (0 when unknown).
`modality` is the modality the signals share, or `"mixed"` when they differ
(for example a turn's text and the photo sent with it under one source), and
the memory's `mimetype` is that of its first signal. Memories come back whole
unless you set an evidence budget.

### Evidence budget

`ProcessOptions.EvidenceBudgetBytes` (zero = whole memories) fits
`standing_memory` plus `retrieved_memory` within a byte budget on any process
call: text counts its UTF-8 bytes, other payloads 480 bytes each, grouped
memories keep their members most similar to the query, and more distinct
memories are kept trimmed before lower-ranked ones are left out (8,000 tokens
at 4.35 chars/token is 34,800 bytes).

```go
ctx, err := engine.ProcessTextWithOptions("When is the marathon long run?", "query/main",
	&cortext.ProcessOptions{Retention: cortext.RetentionEphemeral, EvidenceBudgetBytes: 34_800})
```

### Reference time

A query (`RetentionEphemeral`) call can be asked about a point in time.
`ProcessOptions.ReferenceTime` adds the long-term memories nearest that time
(past or future) to the retrieval candidates; nothing is dropped for being
later. `AsOf: true` also hides memories that start after it. The zero
`ReferenceTime` leaves retrieval unchanged; non-query calls ignore it.

```go
ctx, err := engine.ProcessTextWithOptions("Which pier does the ferry leave from?", "query/main",
	&cortext.ProcessOptions{
		Retention:     cortext.RetentionEphemeral,
		ReferenceTime: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		AsOf:          true,
	})
```

An option the loaded native cannot read (an evidence budget or reference time
on an older engine) returns an error instead of being ignored.

### Text with media

`ProcessTextWithMedia` (and its `JSON` / `AndOptions` variants) encodes the
text but stores `Media.Data` as the signal's payload under `Media.MimeType`.
Nil or empty media stores the text itself. A `text/*` payload is used as text
(keyword index, standing memories, packet text); any other MIME type is an
opaque payload. Natives without the text-media entry point return an error.

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
| `CORTEXT_LI_MODEL_PATH` | Explicit late-interaction reranker `AIST-87M-LI.gguf` |
| `CORTEXT_MODEL_CACHE_DIR` | Where `MaterializeModel` writes reassembled models |

There is no vendored `native/` or `models/` tree and no sibling-checkout search.
Load order:

1. `CORTEXT_LIBRARY_PATH` if set
2. Otherwise `EnsureAssets()` → `native/<platform-tag>/…` from the release tarball

Release natives embed AIST and assemble it on first create. `MaterializeModel`
is optional and always reads from the same release asset tree.

Engines from v5 on also need the late-interaction reranker
`AIST-87M-LI.gguf` whenever AIST resolves, or `New` fails. The engine takes
`CORTEXT_LI_MODEL_PATH`, else the file beside the AIST model, else the copy
linked into the library. `MaterializeModel` checksums the reranker (its own
`manifest.json` entry, sharded beside AIST) and places it beside the
reassembled AIST file, and `New` checks both explicit model paths exist.

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
