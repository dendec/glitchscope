"""Release-archive policy checks for optional preset collections."""
import importlib.util
import hashlib
import sys
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import zipfile

spec = importlib.util.spec_from_file_location(
    'release_packager', Path(__file__).with_name('package-release.py'))
release_packager = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release_packager)


class PresetReleaseTests(unittest.TestCase):
    def test_downloaded_presets_and_legacy_archives_are_excluded(self):
        self.assertTrue(release_packager.include_path(Path('presets'), True))
        for path in ('presets/cream.zip', 'presets/presets.gsa',
                     'presets/textures.gsa', 'presets/.texture-cache/cream/one.jpg'):
            self.assertFalse(release_packager.include_path(Path(path), False), path)

    def test_desktop_zip_layout_and_unix_permissions(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            licenses = base / 'licenses'
            licenses.mkdir()
            (licenses / 'THIRD_PARTY_LICENSES.md').write_text('licenses')
            for platform, library_dir in (('linux-amd64', 'libs.amd64'),
                                          ('linux-arm64', 'libs.aarch64'),
                                          ('windows-amd64', None)):
                source = base / platform
                source.mkdir()
                binary = 'glitchscope' if library_dir else 'glitchscope.exe'
                (source / binary).write_bytes(b'executable')
                (source / binary).chmod(0o755)
                (source / 'README.md').write_text('old staging readme')
                (source / 'settings.json').write_text('{}')
                (source / 'presets').mkdir()
                (source / 'presets/user.zip').write_bytes(b'user preset')
                (source / 'music').mkdir()
                (source / 'music/user.mp3').write_bytes(b'user music')
                cache = source / '.cache/modland'
                cache.mkdir(parents=True)
                (cache / 'catalog').write_bytes(b'catalog')
                if library_dir:
                    libs = source / library_dir
                    libs.mkdir()
                    (libs / 'libstdc++.so.6').write_bytes(b'library')
                    with patch.object(release_packager.subprocess, 'check_output',
                                      return_value='Name: GLIBC_2.9\nName: GLIBC_2.36\n'):
                        extra = release_packager.linux_release_files(source, platform, library_dir)
                    self.assertIn(b'glibc 2.36', extra['README.md'][0])
                else:
                    (source / 'SDL2.dll').write_bytes(b'dll')
                    extra = release_packager.windows_release_files()
                output = base / f'{platform}.zip'
                release_packager.make_zip(source, output, 'app', licenses, extra, library_dir)
                with zipfile.ZipFile(output) as archive:
                    self.assertIsNone(archive.testzip())
                    names = archive.namelist()
                    self.assertEqual(len(names), len(set(names)))
                    for name in ('README.md', 'licenses/THIRD_PARTY_LICENSES.md',
                                 'music/', 'presets/', '.cache/modland/catalog', binary):
                        self.assertIn(f'app/{name}', names)
                    self.assertNotIn('app/settings.json', names)
                    self.assertNotIn('app/music/user.mp3', names)
                    self.assertNotIn('app/presets/user.zip', names)
                    self.assertNotEqual(archive.read('app/README.md'), b'old staging readme')
                    if library_dir:
                        self.assertIn('app/libs/libstdc++.so.6', names)
                        self.assertFalse(any(library_dir in name for name in names))
                        launcher = archive.read('app/run-glitchscope.sh')
                        self.assertIn(b'$app_dir/libs', launcher)
                        self.assertIn(b'"$@"', launcher)
                        mode = archive.getinfo('app/run-glitchscope.sh').external_attr >> 16
                        self.assertEqual(mode & 0o777, 0o755)
                        self.assertIn('app/runtime-requirements.txt', names)
                    else:
                        self.assertIn('app/SDL2.dll', names)

    def test_release_outputs_are_four_zips_with_matching_checksums(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            licenses = base / 'licenses'
            licenses.mkdir()
            (licenses / 'THIRD_PARTY_LICENSES.md').write_text('licenses')
            args = ['package-release.py', '--version', 'test', '--licenses', str(licenses)]
            for platform, library_dir in (('linux-amd64', 'libs.amd64'),
                                          ('linux-arm64', 'libs.aarch64'),
                                          ('windows-amd64', None)):
                source = base / platform
                source.mkdir()
                (source / ('glitchscope' if library_dir else 'glitchscope.exe')).write_bytes(b'executable')
                for relative in release_packager.SHIPPED_CACHE_FILES:
                    path = source.joinpath(*relative)
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_bytes(b'catalog')
                if library_dir:
                    for name in ('libvorbisfile.so.3', 'libvorbis.so.0', 'libogg.so.0',
                                 'libFLAC.so.12', 'libmpg123.so.0', 'libz.so.1',
                                 'libstdc++.so.6', 'libgcc_s.so.1'):
                        path = source / library_dir / name
                        path.parent.mkdir(exist_ok=True)
                        path.write_bytes(b'library')
                else:
                    (source / 'SDL2.dll').write_bytes(b'dll')
                args.extend([f'--{platform}', str(source)])
            portmaster = base / 'portmaster.zip'
            with zipfile.ZipFile(portmaster, 'w') as archive:
                for name in ('GlitchScope.sh', 'glitchscope/glitchscope',
                             'glitchscope/licenses/THIRD_PARTY_LICENSES.md'):
                    archive.writestr(name, b'portmaster')
            output = base / 'releases'
            output.mkdir()
            for platform in ('linux-amd64', 'linux-arm64'):
                (output / f'glitchscope-test-{platform}.tar.gz').write_bytes(b'old archive')
            preserved = output / 'glitchscope-old-linux-amd64.tar.gz'
            preserved.write_bytes(b'previous version')
            args.extend(['--portmaster', str(portmaster), '--output', str(output)])
            with patch.object(sys, 'argv', args), patch.object(
                    release_packager.subprocess, 'check_output', return_value='Name: GLIBC_2.36\n'):
                release_packager.main()
            lines = (output / 'SHA256SUMS').read_text().splitlines()
            self.assertEqual(len(lines), 4)
            for line in lines:
                digest, name = line.split('  ')
                self.assertTrue(name.endswith('.zip'))
                self.assertEqual(digest, hashlib.sha256((output / name).read_bytes()).hexdigest())
                with zipfile.ZipFile(output / name) as archive:
                    self.assertIsNone(archive.testzip())
            self.assertTrue(preserved.exists())
            self.assertEqual(len(list(output.glob('glitchscope-test-*'))), 4)
            self.assertEqual((output / 'glitchscope-test-portmaster.zip').read_bytes(), portmaster.read_bytes())

    def test_portmaster_archive_rejects_downloaded_packs_and_texture_cache(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'release.zip'
            with zipfile.ZipFile(path, 'w') as archive:
                archive.writestr('GlitchScope.sh', '')
                archive.writestr('glitchscope/glitchscope', '')
                archive.writestr('glitchscope/licenses/THIRD_PARTY_LICENSES.md', '')
                archive.writestr('glitchscope/presets/cream-of-the-crop.zip', '')
                archive.writestr('glitchscope/presets/.texture-cache/cream/chess.jpg', '')
            with self.assertRaisesRegex(SystemExit, 'local runtime state'):
                release_packager.validate_portmaster_archive(path)


if __name__ == '__main__':
    unittest.main()
