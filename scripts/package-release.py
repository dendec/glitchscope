#!/usr/bin/env python3
"""Create versioned desktop and PortMaster release archives from staged builds."""

import argparse
import hashlib
import re
import shutil
import stat
import subprocess
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
    if parts[0] == "presets":
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


def make_zip(
    source: Path, output: Path, root_name: str, licenses: Path,
    extra_files: dict[str, tuple[bytes, int]] | None = None,
    library_dir: str | None = None,
) -> None:
    extra_files = extra_files or {}
    with zipfile.ZipFile(output, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=6) as archive:
        add_zip_path(archive, source, root_name)
        # These directories are present in every desktop package, even when the
        # staging build has no local music directory yet.
        for name in ("music", "presets"):
            info = zipfile.ZipInfo(f"{root_name}/{name}/")
            info.external_attr = (stat.S_IFDIR | 0o755) << 16
            archive.writestr(info, b"")
        for path in sorted(source.rglob("*")):
            relative = path.relative_to(source)
            if not include_path(relative, path.is_dir()):
                continue
            if path.is_symlink():
                raise SystemExit(f"Release package must not contain symlinks: {path}")
            if relative.as_posix() in extra_files or relative.as_posix() in {"music", "presets"}:
                continue
            if library_dir and relative.parts[0] == library_dir:
                relative = Path("libs", *relative.parts[1:])
            add_zip_path(archive, path, f"{root_name}/{relative.as_posix()}")
        add_license_tree(archive, licenses, f"{root_name}/licenses")
        for filename, (contents, mode) in extra_files.items():
            info = zipfile.ZipInfo(f"{root_name}/{filename}")
            info.external_attr = (stat.S_IFREG | mode) << 16
            archive.writestr(info, contents, compress_type=zipfile.ZIP_DEFLATED)


def linux_release_files(source: Path, platform: str, library_dir: str) -> dict[str, tuple[bytes, int]]:
    elf_files = [source / "glitchscope", *sorted((source / library_dir).iterdir())]
    requirements = subprocess.check_output(
        ["readelf", "-d", "--version-info", *map(str, elf_files)], text=True)
    requirements = requirements.replace(str(source / library_dir), "libs").replace(
        str(source / "glitchscope"), "glitchscope")
    glibc_versions = re.findall(r"Name: GLIBC_([0-9.]+)", requirements)
    if not glibc_versions:
        raise SystemExit(f"No glibc version requirements found in {source}")
    minimum_glibc = ".".join(map(str, max(tuple(map(int, version.split("."))) for version in glibc_versions)))
    architecture, graphics, graphics_package = {
        "linux-amd64": ("x86-64", "desktop OpenGL and OpenGL ES 2", "libgl1 libgles2"),
        "linux-arm64": ("64-bit ARM", "OpenGL ES 2", "libgles2"),
    }[platform]
    readme = f"""# GlitchScope for Linux {architecture}

Extract this archive and run `./run-glitchscope.sh`. The launcher sets the
library path for the bundled codec and C++ runtime libraries in `libs/`
and starts GlitchScope from this directory. Put local tracks in `music/`.

Requirements: {architecture} Linux, glibc {minimum_glibc} or newer, SDL2,
{graphics}, ALSA, and working audio and graphics drivers supplied by the
system. The archive bundles the codec and C++ runtime libraries. On Debian 12
or compatible systems, install the system libraries with:

```sh
sudo apt update
sudo apt install libsdl2-2.0-0 {graphics_package} libasound2
```

On Debian 13 (Trixie), use `libasound2t64` in place of `libasound2`. See
`runtime-requirements.txt` for the dynamic library and symbol requirements of
the executable and bundled libraries.

PortMaster has a separate launcher and installable archive.
"""
    launcher = """#!/bin/sh
set -eu
app_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$app_dir"
export LD_LIBRARY_PATH="$app_dir/libs${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
exec "$app_dir/glitchscope" "$@"
"""
    return {
        "README.md": (readme.encode("utf-8"), 0o644),
        "run-glitchscope.sh": (launcher.encode("utf-8"), 0o755),
        "runtime-requirements.txt": (requirements.encode("utf-8"), 0o644),
    }


def windows_release_files() -> dict[str, tuple[bytes, int]]:
    readme = """# GlitchScope for Windows x86-64

Extract this archive and start `glitchscope.exe`. Keep `SDL2.dll` beside the
executable. Put local tracks in `music/`; download preset collections from the
Presets page. Requires 64-bit Windows and a graphics driver supporting desktop
OpenGL. Native decoders and the C++ runtime are linked into the executable;
Windows and its device drivers provide the system graphics and audio libraries.
"""
    return {"README.md": (readme.encode("utf-8"), 0o644)}


def validate_portmaster_archive(path: Path) -> None:
    required = {
        "GlitchScope.sh",
        "glitchscope/glitchscope",
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
        elif parts[:2] == ("glitchscope", "presets") and len(parts) > 2:
            forbidden.append(name)
        elif name.endswith(("presets.gsa", "textures.gsa")):
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
    parser.add_argument("--licenses", type=Path, required=True)
    parser.add_argument("--output", type=Path, default=Path("dist/releases"))
    args = parser.parse_args()

    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._+-]*", args.version):
        raise SystemExit("Version may contain only letters, digits, dot, underscore, plus, and hyphen")

    libraries = (
        "libvorbisfile.so.3", "libvorbis.so.0", "libogg.so.0", "libFLAC.so.12",
        "libmpg123.so.0", "libz.so.1", "libstdc++.so.6", "libgcc_s.so.1",
    )
    packages = [
        (args.linux_amd64, "glitchscope", "linux-amd64", "libs.amd64"),
        (args.linux_arm64, "glitchscope", "linux-arm64", "libs.aarch64"),
        (args.windows_amd64, "glitchscope.exe", "windows-amd64", None),
    ]
    catalog_files = (
        ".cache/modland/catalog", ".cache/modarchive/catalog",
        ".cache/modarchive/1980-2007.gsa", ".cache/modarchive/2007-addendum.gsa",
    )
    for source, binary, platform, library_dir in packages:
        if not source.is_dir() or not (source / binary).is_file():
            raise SystemExit(f"Missing {platform} package or executable: {source / binary}")
        platform_files = tuple(f"{library_dir}/{name}" for name in libraries) if library_dir else ("SDL2.dll",)
        for relative in (*catalog_files, *platform_files):
            if not (source / relative).is_file():
                raise SystemExit(f"Missing {platform} release asset: {source / relative}")

    if not args.portmaster.is_file():
        raise SystemExit(f"Missing PortMaster archive: {args.portmaster}")
    if not args.licenses.is_dir() or not (args.licenses / "THIRD_PARTY_LICENSES.md").is_file():
        raise SystemExit(f"Missing collected release license bundle: {args.licenses}")
    validate_portmaster_archive(args.portmaster)

    args.output.mkdir(parents=True, exist_ok=True)
    release_assets = []
    for source, _binary, platform, library_dir in packages:
        root_name = f"glitchscope-{args.version}-{platform}"
        destination = args.output / f"{root_name}.zip"
        extra_files = linux_release_files(source, platform, library_dir) if library_dir else windows_release_files()
        make_zip(source, destination, root_name, args.licenses, extra_files, library_dir)
        print(destination)
        release_assets.append(destination)

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
    # Remove only obsolete archives for this exact release version, once all
    # replacement ZIPs and their checksums have been written successfully.
    for platform in ("linux-amd64", "linux-arm64"):
        (args.output / f"glitchscope-{args.version}-{platform}.tar.gz").unlink(missing_ok=True)


if __name__ == "__main__":
    main()
