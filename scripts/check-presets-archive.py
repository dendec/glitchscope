#!/usr/bin/env python3
"""Check that a portable GSA contains exactly the benchmark-approved presets."""

import csv
import struct
import sys
from pathlib import Path

MIN_FPS = 20.0


def approved_names(benchmark):
    with benchmark.open(newline='') as file:
        reader = csv.DictReader(file)
        expected = ['preset', 'status', 'compile_ms', 'steady_ms_per_frame',
                    'steady_fps', 'total_ms']
        if reader.fieldnames != expected:
            raise ValueError('invalid benchmark CSV header')
        return {row['preset'] for row in reader
                if row['status'] == 'ok' and float(row['steady_fps']) >= MIN_FPS
                and not row['preset'].startswith('!')}


def archive_names(path):
    with path.open('rb') as file:
        if file.read(4) != b'GSA\0':
            raise ValueError('invalid GSA magic')
        version, count = struct.unpack('<II', file.read(8))
        if version != 1 or count > 100_000:
            raise ValueError('invalid GSA version or entry count')
        names = set()
        for _ in range(count):
            size, = struct.unpack('<H', file.read(2))
            if size == 0 or size > 4096:
                raise ValueError('invalid GSA entry name length')
            name = file.read(size).decode('utf-8')
            file.read(4)  # Compressed data length.
            if name in names:
                raise ValueError(f'duplicate GSA entry: {name}')
            names.add(name)
        return names


def check(archive, benchmark):
    expected = approved_names(benchmark)
    actual = archive_names(archive)
    extra, missing = actual - expected, expected - actual
    if extra or missing:
        raise ValueError(f'{len(extra)} unapproved, {len(missing)} approved presets missing '
                         f'(sample: {next(iter(sorted(extra or missing)))})')
    return len(actual)


def main():
    if len(sys.argv) != 3:
        raise SystemExit('usage: check-presets-archive.py ARCHIVE BENCHMARK')
    archive, benchmark = map(Path, sys.argv[1:])
    try:
        count = check(archive, benchmark)
    except (OSError, ValueError, UnicodeError, struct.error) as exc:
        raise SystemExit(f'preset archive check failed: {exc}') from exc
    print(f'validated {count} benchmark-approved presets in {archive}')


if __name__ == '__main__':
    main()
