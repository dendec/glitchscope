#!/usr/bin/env python3
"""Rasterize manifest icons into PNG assets."""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import tempfile
from pathlib import Path


def load_manifest(path: Path) -> tuple[list[int], list[dict[str, object]]]:
    with path.open(encoding="utf-8") as manifest_file:
        manifest = json.load(manifest_file)

    sizes = manifest.get("sizes")
    icons = manifest.get("icons")
    if not isinstance(sizes, list) or not sizes or any(
        not isinstance(size, int) or size <= 0 for size in sizes
    ):
        raise ValueError("manifest sizes must be a non-empty list of positive integers")
    if len(set(sizes)) != len(sizes):
        raise ValueError("manifest sizes must be unique")
    if not isinstance(icons, list) or not icons:
        raise ValueError("manifest icons must be a non-empty list")

    names: set[str] = set()
    normalized: list[dict[str, object]] = []
    for icon in icons:
        if not isinstance(icon, dict):
            raise ValueError("each manifest icon must be an object")
        name = icon.get("name")
        source = icon.get("source")
        if not isinstance(name, str) or not name:
            raise ValueError("each manifest icon needs a non-empty name")
        if not isinstance(source, str) or not source:
            raise ValueError(f"icon {name!r} needs a non-empty source")
        color = icon.get("color", False)
        if not isinstance(color, bool):
            raise ValueError(f"icon {name!r} color must be a boolean")
        if name in names:
            raise ValueError(f"duplicate icon name: {name}")
        names.add(name)
        normalized.append({"name": name, "source": source, "color": color})
    return sizes, normalized


def rasterize(source: Path, destination: Path, size: int, monochrome: bool) -> None:
    with tempfile.NamedTemporaryFile(suffix=".png", dir=destination.parent) as rendered:
        subprocess.run(
            [
                "rsvg-convert",
                "--width",
                str(size),
                "--height",
                str(size),
                "--output",
                rendered.name,
                str(source),
            ],
            check=True,
        )

        from PIL import Image

        with Image.open(rendered.name) as image:
            rgba = image.convert("RGBA")
            if rgba.size != (size, size):
                raise ValueError(
                    f"{source} rendered as {rgba.size[0]}x{rgba.size[1]}, "
                    f"expected {size}x{size}"
                )
            if monochrome:
                alpha = rgba.getchannel("A").point(lambda value: 255 if value >= 128 else 0)
                white = Image.new("L", (size, size), 255)
                rgba = Image.merge("RGBA", (white, white, white, alpha))
            rgba.save(destination, format="PNG", optimize=True)


def source_revision(root: Path) -> str:
    try:
        return subprocess.run(
            ["git", "-C", str(root / "lib/pixelarticons"), "rev-parse", "HEAD"],
            check=True,
            capture_output=True,
            text=True,
        ).stdout.strip()
    except (OSError, subprocess.CalledProcessError):
        return "unknown"


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--manifest", type=Path, default=Path("internal/ui/icon_assets.json"))
    parser.add_argument("--output", type=Path, default=Path("internal/ui/assets/icons"))
    args = parser.parse_args()

    root = Path.cwd()
    manifest_path = args.manifest.resolve()
    output_path = args.output.resolve()
    sizes, icons = load_manifest(manifest_path)
    for icon in icons:
        source = (root / str(icon["source"])).resolve()
        if not source.is_file():
            raise FileNotFoundError(f"icon source does not exist: {source}")

    output_path.parent.mkdir(parents=True, exist_ok=True)
    staging = Path(tempfile.mkdtemp(prefix=".icons-", dir=output_path.parent))
    try:
        for size in sizes:
            size_dir = staging / str(size)
            size_dir.mkdir()
            for icon in icons:
                source = (root / str(icon["source"])).resolve()
                rasterize(
                    source,
                    size_dir / f"{icon['name']}.png",
                    size,
                    monochrome=not bool(icon["color"]),
                )

        generated_manifest = {
            "source_revision": source_revision(root),
            "sizes": sizes,
            "icons": icons,
            "alpha_threshold": 128,
        }
        with (staging / "manifest.json").open("w", encoding="utf-8") as manifest_file:
            json.dump(generated_manifest, manifest_file, indent=2)
            manifest_file.write("\n")

        # mkdtemp creates a private 0700 directory. Published assets must remain
        # readable by the host user when generation runs inside Docker.
        staging.chmod(0o755)
        for generated in staging.rglob("*"):
            generated.chmod(0o755 if generated.is_dir() else 0o644)

        if output_path.exists():
            shutil.rmtree(output_path)
        os.replace(staging, output_path)
    except Exception:
        shutil.rmtree(staging, ignore_errors=True)
        raise


if __name__ == "__main__":
    main()
