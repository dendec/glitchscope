"""Packaging regression checks, independent of Docker/native dependencies."""
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import zipfile
import json

spec = importlib.util.spec_from_file_location('packager', Path(__file__).with_name('package-portmaster.py'))
packager = importlib.util.module_from_spec(spec)
spec.loader.exec_module(packager)


class PackageTests(unittest.TestCase):
    def test_complete_package_and_derived_glibc(self):
        with tempfile.TemporaryDirectory() as temporary:
            base = Path(temporary)
            arm = base / 'arm'
            files = ['glitchscope', 'presets/presets.gsa', 'presets/textures.gsa',
                     'licenses/GlitchScope-GPL-2.0-or-later.txt', 'licenses/libopenmpt-LICENSE',
                     'licenses/libxmp-COPYING', 'libs.aarch64/libstdc++.so.6',
                     'libs.aarch64/libvorbisfile.so.3', '.cache/modland/catalog',
                     '.cache/modarchive/catalog', '.cache/modarchive/1980-2007.gsa',
                     '.cache/modarchive/2007-addendum.gsa']
            for name in files:
                path = arm / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(b'\x7fELF\x02\x01' + bytes(12) + b'\xb7\x00')
            (base / 'LICENSE.md').write_text('preset notice')
            (base / 'README.md').write_text('texture notice')
            args = ['package', '--arm64', str(arm), '--presets', str(base),
                    '--textures', str(base), '--output', str(base / 'out')]
            with patch('sys.argv', args), patch.object(packager.subprocess, 'check_output',
                    side_effect=['Name: GLIBC_2.9\nName: GLIBC_2.36\n', 'NEEDED']):
                packager.main()
            root = base / 'out/portmaster-submit/ports/glitchscope'
            for name in ['README.md', 'port.json', 'gameinfo.xml', 'screenshot.png', 'GlitchScope.sh']:
                self.assertTrue((root / name).is_file(), name)
            with zipfile.ZipFile(base / 'out/glitchscope.zip') as archive:
                self.assertIsNone(archive.testzip())
                self.assertIn('glitchscope/music/', archive.namelist())
                self.assertIn('glitchscope/.cache/modland/catalog', archive.namelist())
                self.assertIn('glitchscope/licenses/presets-LICENSE.md', archive.namelist())
                self.assertEqual(json.loads(archive.read('glitchscope/port.json'))['attr']['min_glibc'], '2.36')
                self.assertEqual(archive.getinfo('GlitchScope.sh').external_attr >> 16 & 0o777, 0o755)
                self.assertFalse(any(name.endswith(('.s3m', '.xm', '.it', '.mod')) for name in archive.namelist()))

    def test_missing_input_does_not_replace_existing_archive(self):
        with tempfile.TemporaryDirectory() as temporary:
            base = Path(temporary)
            archive = base / 'glitchscope.zip'
            archive.write_bytes(b'previous release')
            args = ['package', '--arm64', str(base / 'missing'), '--presets', str(base),
                    '--textures', str(base), '--output', str(base)]
            with patch('sys.argv', args), self.assertRaisesRegex(SystemExit, 'Missing ARM64 release input'):
                packager.main()
            self.assertEqual(archive.read_bytes(), b'previous release')


if __name__ == '__main__':
    unittest.main()
