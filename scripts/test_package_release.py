"""Release-archive policy checks for optional preset collections."""
import importlib.util
from pathlib import Path
import tempfile
import unittest
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
