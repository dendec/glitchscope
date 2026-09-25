#!/usr/bin/env python3
"""Create versioned desktop and PortMaster release archives from staged builds."""

import argparse
import hashlib
import io
import re
import shutil
import stat
import tarfile
import zipfile
from pathlib import Path, PurePosixPath


SHIPPED_CACHE_FILES = {
    (".cache", "modland", "catalog"),
    (".cache", "modarchive", "catalog"),
    (".cache", "modarchive", "1980-2007.gsa"),
    (".cache", "modarchive", "2007-addendum.gsa"),
    (".cache", "shuffle", "modland.idx"),
    (".cache", "shuffle", "modarchive.idx"),
}
MUTABLE_FILES = {"settings.json", "favorites.json"}


def include_path(relative: Path, is_dir: bool) -> bool:
    parts = relative.parts
    if not parts:
        return False
    if parts[0] == "licenses":
        return False
    if parts[0] == "music":
        return is_dir and len(parts) == 1
    if relative.name in MUTABLE_FILES:
        return False
    if parts[0] == ".cache":
        key = tuple(parts)
        if is_dir:
            return any(item[:len(key)] == key for item in SHIPPED_CACHE_FILES)
        return key in SHIPPED_CACHE_FILES
    return True


def add_zip_path(archive: zipfile.ZipFile, path: Path, name: str) -> None:
    mode = stat.S_IMODE(path.stat().st_mode)
    if path.is_dir():
        info = zipfile.ZipInfo(name.rstrip("/") + "/")
        info.external_attr = (stat.S_IFDIR | mode) << 16
        archive.writestr(info, b"")
        return
    info = zipfile.ZipInfo(name)
    info.external_attr = (stat.S_IFREG | mode) << 16
    archive.writestr(info, path.read_bytes(), compress_type=zipfile.ZIP_DEFLATED)


def add_license_tree(archive: zipfile.ZipFile, licenses: Path, root_name: str) -> None:
    for path in [licenses, *sorted(licenses.rglob("*"))]:
        if path.is_symlink():
            raise SystemExit(f"License bundle must not contain symlinks: {path}")
        relative = path.relative_to(licenses)
        name = root_name if not relative.parts else f"{root_name}/{relative.as_posix()}"
        add_zip_path(archive, path, name)


def make_zip(source: Path, output: Path, root_name: str, licenses: Path) -> None:
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=6) as archive:
        add_zip_path(archive, source, root_name)
        for path in sorted(source.rglob("*")):
            if path.is_symlink():
                raise SystemExit(f"Release package must not contain symlinks: {path}")
            relative = path.relative_to(source)
            if include_path(relative, path.is_dir()):
                add_zip_path(archive, path, f"{root_name}/{relative.as_posix()}")
        add_license_tree(archive, licenses, f"{root_name}/licenses")


def make_tar_gz(
    source: Path,
    output: Path,
    root_name: str,
    licenses: Path,
    extra_files: dict[str, tuple[bytes, int]] | None = None,
    excluded_paths: set[str] | None = None,
) -> None:
    extra_files = extra_files or {}
    excluded_paths = excluded_paths or set()
    with tarfile.open(output, "w:gz", format=tarfile.PAX_FORMAT) as archive:
        archive.add(source, arcname=root_name, recursive=False)
        paths = sorted(source.rglob("*"))
        paths.extend([licenses, *sorted(licenses.rglob("*"))])
        for path in paths:
            if path.is_symlink():
                raise SystemExit(f"Release package must not contain symlinks: {path}")
            if path.is_relative_to(source):
                relative = path.relative_to(source)
                if relative.as_posix() in excluded_paths or not include_path(relative, path.is_dir()):
                    continue
                name = PurePosixPath(root_name, relative.as_posix())
            else:
                relative = path.relative_to(licenses)
                name = PurePosixPath(root_name, "licenses", relative.as_posix())
            archive.add(path, arcname=str(name), recursive=False)
        for filename, (contents, mode) in extra_files.items():
            info = tarfile.TarInfo(f"{root_name}/{filename}")
            info.mode = mode
            info.size = len(contents)
            archive.addfile(info, io.BytesIO(contents))


def linux_arm64_release_files(runtime_requirements: Path) -> dict[str, tuple[bytes, int]]:
    requirements = runtime_requirements.read_text(encoding="utf-8")
    glibc_versions = re.findall(r"Name: GLIBC_([0-9.]+)", requirements)
    if not glibc_versions:
        raise SystemExit(f"No glibc version requirements found in {runtime_requirements}")
    minimum_glibc = ".".join(map(str, max(tuple(map(int, version.split("."))) for version in glibc_versions)))

    readme = f"""# GlitchScope for Linux ARM64

Extract this archive and run `./run-glitchscope.sh`. The launcher sets the
library path for the bundled codec and C++ runtime libraries in `libs.aarch64/`
and starts GlitchScope from this directory. Put local tracks in `music/`.

Requirements: 64-bit ARM Linux, glibc {minimum_glibc} or newer, SDL2,
OpenGL ES 2, ALSA, and working audio and graphics drivers supplied by the
system. The archive bundles the codec and C++ runtime libraries. On Debian 12
or 64-bit Raspberry Pi OS Bookworm, install the system libraries with:

```sh
sudo apt update
sudo apt install libsdl2-2.0-0 libgles2 libasound2
```

On Debian 13 (Trixie), use `libasound2t64` in place of `libasound2`. See
`runtime-requirements.txt` for the dynamic library and symbol requirements of
the executable and bundled libraries.

This is the regular Linux ARM64 package. PortMaster has a separate launcher and
installable archive.
"""
    launcher = """#!/bin/sh
set -eu
app_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$app_dir"
export LD_LIBRARY_PATH="$app_dir/libs.aarch64${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
exec "$app_dir/glitchscope" "$@"
"""
    return {
        "README.md": (readme.encode("utf-8"), 0o644),
        "run-glitchscope.sh": (launcher.encode("utf-8"), 0o755),
        "runtime-requirements.txt": (requirements.encode("utf-8"), 0o644),
    }


def validate_portmaster_archive(path: Path) -> None:
    required = {
        "GlitchScope.sh",
        "glitchscope/glitchscope",
        "glitchscope/presets/presets.gsa",
        "glitchscope/presets/textures.gsa",
        "glitchscope/licenses/THIRD_PARTY_LICENSES.md",
    }
    with zipfile.ZipFile(path) as archive:
        names = set(archive.namelist())
    missing = sorted(required - names)
    if missing:
        raise SystemExit(f"PortMaster archive is missing required entries: {', '.join(missing)}")
    forbidden = []
    for name in names:
        parts = PurePosixPath(name).parts
        if any(part in MUTABLE_FILES for part in parts):
            forbidden.append(name)
        elif "music" in parts and not name.endswith("/"):
            forbidden.append(name)
        elif name.endswith((".cache/shuffle/cached-tracks.json",)):
            forbidden.append(name)
    if forbidden:
        raise SystemExit(f"PortMaster archive contains local runtime state: {', '.join(sorted(forbidden))}")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--linux-amd64", type=Path, required=True)
    parser.add_argument("--linux-arm64", type=Path, required=True)
    parser.add_argument("--windows-amd64", type=Path, required=True)
    parser.add_argument("--portmaster", type=Path, required=True)
    parser.add_argument("--arm64-runtime-requirements", type=Path, required=True)
    parser.add_argument("--licenses", type=Path, required=True)
    parser.add_argument("--output", type=Path, default=Path("dist/releases"))
    args = parser.parse_args()

    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._+-]*", args.version):
        raise SystemExit("Version may contain only letters, digits, dot, underscore, plus, and hyphen")

    packages = [
        (
            args.linux_amd64,
            "glitchscope",
            "linux-amd64",
            ".tar.gz",
            (),
        ),
        (
            args.windows_amd64,
            "glitchscope.exe",
            "windows-amd64",
            ".zip",
            ("SDL2.dll",),
        ),
    ]
    catalog_files = (
        ".cache/modland/catalog",
        ".cache/modarchive/catalog",
        ".cache/modarchive/1980-2007.gsa",
        ".cache/modarchive/2007-addendum.gsa",
    )
    for source, binary, platform, _suffix, platform_files in packages:
        if not source.is_dir() or not (source / binary).is_file():
            raise SystemExit(f"Missing {platform} package or executable: {source / binary}")
        for relative in (
            "presets/presets.gsa",
            "presets/textures.gsa",
            *catalog_files,
            *platform_files,
        ):
            if not (source / relative).is_file():
                raise SystemExit(f"Missing {platform} release asset: {source / relative}")

    arm64 = args.linux_arm64
    if not arm64.is_dir() or not (arm64 / "glitchscope").is_file():
        raise SystemExit(f"Missing Linux ARM64 package or executable: {arm64 / 'glitchscope'}")
    required_arm64_files = (
        "presets/presets.gsa",
        "presets/textures.gsa",
        ".cache/modland/catalog",
        ".cache/modarchive/catalog",
        ".cache/modarchive/1980-2007.gsa",
        ".cache/modarchive/2007-addendum.gsa",
        "libs.aarch64/libstdc++.so.6",
        "libs.aarch64/libvorbisfile.so.3",
    )
    for relative in required_arm64_files:
        if not (arm64 / relative).is_file():
            raise SystemExit(f"Missing Linux ARM64 release asset: {arm64 / relative}")
    if not args.arm64_runtime_requirements.is_file():
        raise SystemExit(f"Missing Linux ARM64 runtime requirements: {args.arm64_runtime_requirements}")

    if not args.portmaster.is_file():
        raise SystemExit(f"Missing PortMaster archive: {args.portmaster}")
    if not args.licenses.is_dir() or not (args.licenses / "THIRD_PARTY_LICENSES.md").is_file():
        raise SystemExit(f"Missing collected release license bundle: {args.licenses}")
    validate_portmaster_archive(args.portmaster)

    args.output.mkdir(parents=True, exist_ok=True)
    release_assets = []
    for source, _binary, platform, suffix, _platform_files in packages:
        root_name = f"glitchscope-{args.version}-{platform}"
        destination = args.output / f"{root_name}{suffix}"
        if suffix == ".tar.gz":
            make_tar_gz(source, destination, root_name, args.licenses)
        else:
            make_zip(source, destination, root_name, args.licenses)
        print(destination)
        release_assets.append(destination)

    arm64_root = f"glitchscope-{args.version}-linux-arm64"
    arm64_output = args.output / f"{arm64_root}.tar.gz"
    make_tar_gz(
        arm64,
        arm64_output,
        arm64_root,
        args.licenses,
        extra_files=linux_arm64_release_files(args.arm64_runtime_requirements),
        excluded_paths={"README.md"},
    )
    print(arm64_output)
    release_assets.append(arm64_output)

    portmaster_output = args.output / f"glitchscope-{args.version}-portmaster.zip"
    shutil.copy2(args.portmaster, portmaster_output)
    print(portmaster_output)
    release_assets.append(portmaster_output)

    checksums = []
    for path in release_assets:
        digest = hashlib.sha256()
        with path.open("rb") as file:
            for chunk in iter(lambda: file.read(1024 * 1024), b""):
                digest.update(chunk)
        checksums.append(f"{digest.hexdigest()}  {path.name}")
    checksum_file = args.output / "SHA256SUMS"
    checksum_file.write_text("\n".join(checksums) + "\n", encoding="utf-8")
    print(checksum_file)


if __name__ == "__main__":
    main()
