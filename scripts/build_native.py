#!/usr/bin/env python3
"""Build Zig cross-arch shared libraries into cortext.go/native/.

By default this looks for a sibling `../cortext` source tree (the augmem/cortext
repo) and runs `zig build -Dshared=true` for each target, matching
cortext/scripts/build_python_package.py.

Examples:
  python3 scripts/build_native.py
  python3 scripts/build_native.py --target macos-aarch64
  python3 scripts/build_native.py --cortext-root /path/to/cortext --skip-zig-build
"""
from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path


PACKAGE_ROOT = Path(__file__).resolve().parents[1]
PACKAGE_NATIVE = PACKAGE_ROOT / "native"
DEFAULT_CORTEXT = PACKAGE_ROOT.parent / "cortext"


@dataclass(frozen=True, slots=True)
class Target:
    package_tag: str
    zig_target: str
    artifact_name: str
    artifact_globs: tuple[str, ...]
    strip_tools: tuple[str, ...] = ()


TARGETS = (
    Target(
        "linux-x86_64",
        "x86_64-linux-gnu.2.17",
        "libcortext.so",
        ("lib/libcortext.so*",),
        ("x86_64-linux-gnu-strip", "strip"),
    ),
    Target(
        "linux-aarch64",
        "aarch64-linux-gnu.2.17",
        "libcortext.so",
        ("lib/libcortext.so*",),
        ("aarch64-linux-gnu-strip",),
    ),
    Target(
        "macos-x86_64",
        "x86_64-macos",
        "libcortext.dylib",
        ("lib/libcortext*.dylib",),
    ),
    Target(
        "macos-aarch64",
        "aarch64-macos",
        "libcortext.dylib",
        ("lib/libcortext*.dylib",),
    ),
    Target(
        "windows-x86_64",
        "x86_64-windows-gnu",
        "cortext.dll",
        ("bin/cortext.dll", "bin/libcortext.dll"),
    ),
    Target(
        "windows-aarch64",
        "aarch64-windows-gnu",
        "cortext.dll",
        ("bin/cortext.dll", "bin/libcortext.dll"),
    ),
)


def run(cmd: list[str], *, cwd: Path) -> None:
    print("+ " + " ".join(cmd), flush=True)
    subprocess.run(cmd, cwd=cwd, check=True)


def select_targets(names: list[str]) -> list[Target]:
    if not names:
        return list(TARGETS)
    by_name = {target.package_tag: target for target in TARGETS}
    selected: list[Target] = []
    for name in names:
        try:
            selected.append(by_name[name])
        except KeyError as exc:
            supported = ", ".join(sorted(by_name))
            raise SystemExit(f"unknown target {name!r}; supported: {supported}") from exc
    return selected


def find_artifact(prefix: Path, target: Target) -> Path:
    matches: list[Path] = []
    for pattern in target.artifact_globs:
        matches.extend(
            path for path in prefix.glob(pattern) if path.is_file() and not path.is_symlink()
        )
    if not matches:
        searched = ", ".join(str(prefix / pattern) for pattern in target.artifact_globs)
        raise FileNotFoundError(
            f"could not find {target.package_tag} shared library; searched {searched}"
        )
    return max(matches, key=lambda path: (path.stat().st_size, len(path.name), path.name))


def maybe_strip(path: Path, target: Target) -> None:
    for tool in target.strip_tools:
        resolved = shutil.which(tool)
        if resolved is None:
            continue
        try:
            run([resolved, "-s", str(path)], cwd=path.parent)
            return
        except subprocess.CalledProcessError:
            continue


def build_native(
    cortext_root: Path,
    target: Target,
    zig: str,
    optimize: str,
    skip_zig_build: bool,
    build_root: Path,
) -> dict[str, object]:
    prefix = build_root / "prefix" / target.package_tag
    cache = build_root / "zig-cache" / target.package_tag
    global_cache = build_root / "zig-global-cache"
    if not skip_zig_build and prefix.exists():
        shutil.rmtree(prefix)
    prefix.mkdir(parents=True, exist_ok=True)
    cache.mkdir(parents=True, exist_ok=True)
    global_cache.mkdir(parents=True, exist_ok=True)

    if not skip_zig_build:
        run(
            [
                zig,
                "build",
                "--prefix",
                str(prefix),
                "--cache-dir",
                str(cache),
                "--global-cache-dir",
                str(global_cache),
                f"-Dtarget={target.zig_target}",
                f"-Doptimize={optimize}",
                "-Dshared=true",
                "-Dcli=false",
                "-Dfetch-aist-model=false",
            ],
            cwd=cortext_root,
        )

    artifact = find_artifact(prefix, target)
    target_dir = PACKAGE_NATIVE / target.package_tag
    target_dir.mkdir(parents=True, exist_ok=True)
    bundled = target_dir / target.artifact_name
    shutil.copy2(artifact, bundled)
    maybe_strip(bundled, target)
    size = bundled.stat().st_size
    print(f"bundled {target.package_tag}: {bundled} ({size / (1024 * 1024):.2f} MiB)", flush=True)
    return {
        "package_tag": target.package_tag,
        "zig_target": target.zig_target,
        "artifact": target.artifact_name,
        "size": size,
    }


def copy_from_python_package(cortext_root: Path, targets: list[Target]) -> list[dict[str, object]]:
    """Reuse already-built Python wheel natives when present."""
    src_root = cortext_root / "bindings" / "python" / "augmem" / "cortext" / "native"
    entries: list[dict[str, object]] = []
    for target in targets:
        src = src_root / target.package_tag / target.artifact_name
        if not src.is_file():
            raise FileNotFoundError(f"missing prebuilt native at {src}")
        dest_dir = PACKAGE_NATIVE / target.package_tag
        dest_dir.mkdir(parents=True, exist_ok=True)
        dest = dest_dir / target.artifact_name
        shutil.copy2(src, dest)
        size = dest.stat().st_size
        print(f"copied {target.package_tag}: {dest} ({size / (1024 * 1024):.2f} MiB)", flush=True)
        entries.append(
            {
                "package_tag": target.package_tag,
                "zig_target": target.zig_target,
                "artifact": target.artifact_name,
                "size": size,
            }
        )
    return entries


def write_manifest(entries: list[dict[str, object]], optimize: str) -> None:
    manifest = {
        "schema": "augmem.cortext.native.v1",
        "optimize": optimize,
        "targets": entries,
    }
    PACKAGE_NATIVE.mkdir(parents=True, exist_ok=True)
    (PACKAGE_NATIVE / "manifest.json").write_text(
        json.dumps(manifest, indent=2) + "\n", encoding="utf-8"
    )


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Build/bundle Zig cortext natives for the Go package.")
    parser.add_argument(
        "--cortext-root",
        type=Path,
        default=Path(os.environ.get("CORTEXT_ROOT", DEFAULT_CORTEXT)),
        help="Path to augmem/cortext source (default: ../cortext).",
    )
    parser.add_argument("--zig", default=os.environ.get("ZIG", "zig"), help="Zig executable.")
    parser.add_argument(
        "--target",
        action="append",
        default=[],
        help="Package target tag. May be repeated. Default: all.",
    )
    parser.add_argument(
        "--optimize",
        default="ReleaseSmall",
        choices=("Debug", "ReleaseSafe", "ReleaseFast", "ReleaseSmall"),
    )
    parser.add_argument(
        "--skip-zig-build",
        action="store_true",
        help="Reuse existing prefixes under build/native.",
    )
    parser.add_argument(
        "--from-python-natives",
        action="store_true",
        help="Copy natives from cortext/bindings/python/.../native instead of zig build.",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    cortext_root = args.cortext_root.resolve()
    if not (cortext_root / "build.zig").is_file():
        raise SystemExit(f"cortext root missing build.zig: {cortext_root}")

    targets = select_targets(args.target)
    PACKAGE_NATIVE.mkdir(parents=True, exist_ok=True)

    if args.from_python_natives:
        entries = copy_from_python_package(cortext_root, targets)
    else:
        build_root = PACKAGE_ROOT / "build" / "native"
        entries = [
            build_native(
                cortext_root,
                target,
                args.zig,
                args.optimize,
                args.skip_zig_build,
                build_root,
            )
            for target in targets
        ]
    write_manifest(entries, args.optimize if not args.from_python_natives else "ReleaseSmall")
    print(f"wrote {PACKAGE_NATIVE / 'manifest.json'}", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
