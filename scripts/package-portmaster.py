#!/usr/bin/env python3
"""Stage PortMaster-New metadata and build an installable ZIP from ARM64 output."""
import argparse
import json
import re
from pathlib import Path
import shutil
import subprocess
import zipfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--arm64', type=Path, required=True)
    parser.add_argument('--presets', type=Path, required=True)
    parser.add_argument('--textures', type=Path, required=True)
    parser.add_argument('--output', type=Path, default=Path('dist'))
    args = parser.parse_args()
    source = Path('portmaster')
    required = ['glitchscope', 'presets/presets.gsa', 'presets/textures.gsa',
                'licenses/GlitchScope-GPL-2.0-or-later.txt',
                'licenses/libopenmpt-LICENSE', 'licenses/libxmp-COPYING',
                'libs.aarch64/libstdc++.so.6', 'libs.aarch64/libvorbisfile.so.3',
                '.cache/modland/catalog', '.cache/modarchive/catalog',
                '.cache/modarchive/1980-2007.gsa', '.cache/modarchive/2007-addendum.gsa']
    for name in required:
        if not (args.arm64 / name).is_file():
            raise SystemExit(f'Missing ARM64 release input: {args.arm64 / name}')
    elf_inputs = [args.arm64 / 'glitchscope', *sorted((args.arm64 / 'libs.aarch64').iterdir())]
    for path in elf_inputs:
        with path.open('rb') as input_file:
            header = input_file.read(20)
        if header[:6] != b'\x7fELF\x02\x01' or header[18:20] != b'\xb7\x00':
            raise SystemExit(f'Expected a little-endian ARM64 ELF: {path}')
    root = args.output / 'portmaster-submit' / 'ports' / 'glitchscope'
    if root.exists():
        shutil.rmtree(root)
    game = root / 'glitchscope'
    game.mkdir(parents=True)
    for name in ('presets', '.cache', 'licenses', 'libs.aarch64'):
        shutil.copytree(args.arm64 / name, game / name)
    shutil.copy2(args.arm64 / 'glitchscope', game / 'glitchscope')
    (game / 'glitchscope').chmod(0o755)
    (game / 'music').mkdir()
    for name in ('port.json', 'README.md', 'gameinfo.xml', 'screenshot.png', 'GlitchScope.sh'):
        shutil.copy2(source / name, root / name)
    (root / 'GlitchScope.sh').chmod(0o755)
    # Metadata is at submission root; installed copies support offline reading
    # and the image path used by gameinfo.xml.
    for name in ('port.json', 'README.md', 'gameinfo.xml', 'screenshot.png'):
        shutil.copy2(root / name, game / name)
    shutil.copy2(args.presets / 'LICENSE.md', game / 'licenses/presets-LICENSE.md')
    shutil.copy2(args.textures / 'README.md', game / 'licenses/textures-README.md')
    elf_files = [str(game / 'glitchscope'), *map(str, sorted((game / 'libs.aarch64').iterdir()))]
    versions = subprocess.check_output(['readelf', '--version-info', *elf_files], text=True)
    dynamic = subprocess.check_output(['readelf', '-d', *elf_files], text=True)
    (game / 'runtime-requirements.txt').write_text(dynamic + '\n' + versions)
    metadata = json.loads((root / 'port.json').read_text())
    required_versions = [tuple(map(int, value.split('.'))) for value in re.findall(r'Name: GLIBC_([0-9.]+)', versions)]
    if not required_versions:
        raise SystemExit('No glibc version requirements found in ARM64 release')
    metadata['attr']['min_glibc'] = '.'.join(map(str, max(required_versions)))
    for destination in (root / 'port.json', game / 'port.json'):
        destination.write_text(json.dumps(metadata, indent=2) + '\n')
    archive = args.output / metadata['name']
    with zipfile.ZipFile(archive, 'w', zipfile.ZIP_DEFLATED) as output:
        output.write(root / 'GlitchScope.sh', 'GlitchScope.sh')
        for path in sorted(game.rglob('*')):
            output.write(path, path.relative_to(root))
    print(f'Installable ZIP: {archive}\nSubmission directory: {root}')
    print('Review asset permissions and test CFW compatibility before submission; see docs/PORTMASTER-RELEASE.md.')


if __name__ == '__main__':
    main()
