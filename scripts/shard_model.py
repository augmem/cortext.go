#!/usr/bin/env python3
"""Split the AIST GGUF model into Git-friendly chunks for models/.

Mirrors cortext-hermes-plugin: parts stay under 100 MiB (default 48 MiB) so they
can live in ordinary Git without LFS. Writes models/manifest.json with per-chunk
and whole-file SHA-256 digests.

Examples:
  python3 scripts/shard_model.py
  python3 scripts/shard_model.py --source ../cortext/models/AIST-87M-GGUF/AIST-87M_q8_0.gguf
  python3 scripts/shard_model.py --chunk-size $((48 * 1024 * 1024))
"""
from __future__ import annotations

import argparse
import hashlib
import json
import shutil
import sys
from pathlib import Path

PACKAGE_ROOT = Path(__file__).resolve().parents[1]
MODELS = PACKAGE_ROOT / "models"
DEFAULT_SOURCE_CANDIDATES = (
    PACKAGE_ROOT.parent / "cortext" / "models" / "AIST-87M-GGUF" / "AIST-87M_q8_0.gguf",
    PACKAGE_ROOT.parent / "cortext-hermes-plugin" / "vendor" / "models" / "AIST-87M-GGUF" / "chunks",
)
MODEL_REL = "AIST-87M-GGUF/AIST-87M_q8_0.gguf"
VOCAB_REL = "mdbr-leaf-ir/vocab.txt"
DEFAULT_CHUNK = 48 * 1024 * 1024  # same as hermes plugin parts


def digest_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()


def find_source(explicit: Path | None) -> Path:
    if explicit is not None:
        return explicit.resolve()
    for candidate in DEFAULT_SOURCE_CANDIDATES:
        if candidate.is_file() or candidate.is_dir():
            return candidate.resolve()
    raise SystemExit(
        "could not locate AIST model; pass --source path/to/AIST-87M_q8_0.gguf "
        "or a directory of existing .part-* chunks"
    )


def copy_vocab() -> dict[str, object]:
    candidates = (
        PACKAGE_ROOT.parent / "cortext" / "models" / "mdbr-leaf-ir" / "vocab.txt",
        PACKAGE_ROOT.parent / "cortext-hermes-plugin" / "vendor" / "models" / "mdbr-leaf-ir" / "vocab.txt",
        MODELS / VOCAB_REL,
    )
    src = next((p for p in candidates if p.is_file()), None)
    if src is None:
        raise SystemExit("missing vocab.txt; expected under ../cortext/models/mdbr-leaf-ir/")
    dest = MODELS / VOCAB_REL
    dest.parent.mkdir(parents=True, exist_ok=True)
    if src.resolve() != dest.resolve():
        shutil.copy2(src, dest)
    return {"filename": VOCAB_REL, "sha256": digest_file(dest)}


def shard_from_file(source: Path, chunk_size: int) -> tuple[list[dict[str, object]], str, int]:
    chunk_dir = MODELS / "AIST-87M-GGUF" / "chunks"
    if chunk_dir.exists():
        shutil.rmtree(chunk_dir)
    chunk_dir.mkdir(parents=True, exist_ok=True)

    whole = hashlib.sha256()
    chunks: list[dict[str, object]] = []
    index = 0
    total = 0
    with source.open("rb") as handle:
        while True:
            data = handle.read(chunk_size)
            if not data:
                break
            name = f"AIST-87M_q8_0.gguf.part-{index:03d}"
            rel = f"AIST-87M-GGUF/chunks/{name}"
            path = MODELS / rel
            path.write_bytes(data)
            part_hash = hashlib.sha256(data).hexdigest()
            whole.update(data)
            chunks.append({"filename": rel, "sha256": part_hash})
            total += len(data)
            print(f"wrote {rel} ({len(data) / (1024 * 1024):.2f} MiB) {part_hash}", flush=True)
            index += 1
    return chunks, whole.hexdigest(), total


def adopt_existing_chunks(chunk_dir: Path) -> tuple[list[dict[str, object]], str, int]:
    parts = sorted(chunk_dir.glob("AIST-87M_q8_0.gguf.part-*"))
    if not parts:
        raise SystemExit(f"no part files in {chunk_dir}")
    dest_dir = MODELS / "AIST-87M-GGUF" / "chunks"
    dest_dir.mkdir(parents=True, exist_ok=True)
    whole = hashlib.sha256()
    chunks: list[dict[str, object]] = []
    total = 0
    for part in parts:
        dest = dest_dir / part.name
        if part.resolve() != dest.resolve():
            shutil.copy2(part, dest)
        data_hash = digest_file(dest)
        with dest.open("rb") as handle:
            for block in iter(lambda: handle.read(1024 * 1024), b""):
                whole.update(block)
        rel = f"AIST-87M-GGUF/chunks/{part.name}"
        size = dest.stat().st_size
        total += size
        chunks.append({"filename": rel, "sha256": data_hash})
        print(f"adopted {rel} ({size / (1024 * 1024):.2f} MiB) {data_hash}", flush=True)
    return chunks, whole.hexdigest(), total


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, default=None, help="Full .gguf or directory of .part-* chunks")
    parser.add_argument("--chunk-size", type=int, default=DEFAULT_CHUNK, help="Bytes per part (default 48 MiB)")
    parser.add_argument("--version", default="1.2.0", help="cortext_version recorded in manifest")
    args = parser.parse_args()

    source = find_source(args.source)
    MODELS.mkdir(parents=True, exist_ok=True)

    if source.is_dir():
        chunks, model_sha, size = adopt_existing_chunks(source)
    else:
        chunks, model_sha, size = shard_from_file(source, args.chunk_size)

    vocab = copy_vocab()
    manifest = {
        "schema": "augmem.cortext.models.v1",
        "cortext_version": args.version,
        "note": (
            "AIST model is Git-chunked under 100 MiB so it can live in ordinary "
            "Git without LFS. Reassembled and checksummed on first use."
        ),
        "assets": [
            {
                "filename": MODEL_REL,
                "sha256": model_sha,
                "size": size,
                "chunks": chunks,
            },
            vocab,
        ],
    }
    out = MODELS / "manifest.json"
    out.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    print(f"wrote {out}", flush=True)
    print(f"model sha256={model_sha} size={size}", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
