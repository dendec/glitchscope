"""Regression checks for the portable preset archive build gate."""

import csv
import importlib.util
import struct
import tempfile
import unittest
from pathlib import Path


spec = importlib.util.spec_from_file_location(
    'check_presets_archive', Path(__file__).with_name('check-presets-archive.py'))
checker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checker)


class PresetArchiveCheckTest(unittest.TestCase):
    def test_rejects_old_unfiltered_archive(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            benchmark = root / 'benchmark.csv'
            with benchmark.open('w', newline='') as file:
                writer = csv.writer(file)
                writer.writerow(['preset', 'status', 'compile_ms',
                                 'steady_ms_per_frame', 'steady_fps', 'total_ms'])
                writer.writerow(['fast.milk', 'ok', 1, 1, 25, 1])
                writer.writerow(['slow.milk', 'ok', 1, 1, 1, 1])
            archive = root / 'presets.gsa'
            with archive.open('wb') as file:
                file.write(b'GSA\0' + struct.pack('<II', 1, 2))
                for name in ('fast.milk', 'slow.milk'):
                    encoded = name.encode()
                    file.write(struct.pack('<H', len(encoded)) + encoded + struct.pack('<I', 0))
            with self.assertRaisesRegex(ValueError, '1 unapproved'):
                checker.check(archive, benchmark)


if __name__ == '__main__':
    unittest.main()
