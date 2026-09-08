#!/usr/bin/env python3
"""Translate the compact UI catalog with the local Hy-MT2 server."""

from __future__ import annotations

import argparse
import json
from pathlib import Path

import translate_help


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--lang", required=True)
    parser.add_argument("--base-url", required=True)
    parser.add_argument("--input", type=Path, default=Path("internal/i18n/assets/en.json"))
    parser.add_argument("--output", type=Path)
    parser.add_argument("--timeout", type=float, default=180.0)
    args = parser.parse_args()

    target = translate_help.LANGUAGE_NAMES.get(args.lang, args.lang)
    source = json.loads(args.input.read_text(encoding="utf-8"))
    prompt = f"""[Background Information]
This is the complete compact UI string catalog for the GlitchScope music player.

### Task
Translate every JSON value into natural, concise {target} suitable for a small handheld screen.

### Strict Rules
1. Preserve every JSON key exactly.
2. Preserve product names and technical abbreviations.
3. Keep values short; use standard terminology found in media-player interfaces.
4. Output only a valid JSON object without Markdown or explanation.

### Source Data
{json.dumps(source, ensure_ascii=False, indent=2)}"""
    response = translate_help.request_prompt(
        args.base_url.rstrip("/"), prompt, args.timeout, 4096, json_mode=True
    )
    translated = translate_help.parse_json_response(response)
    if translated.keys() != source.keys():
        missing = source.keys() - translated.keys()
        extra = translated.keys() - source.keys()
        raise ValueError(f"catalog keys changed; missing={sorted(missing)}, extra={sorted(extra)}")
    if any(not isinstance(value, str) or not value for value in translated.values()):
        raise ValueError("catalog contains an empty or non-string value")
    output = args.output or Path("internal/i18n/assets") / f"{args.lang}.json"
    translate_help.atomic_write(output, translated)
    print(f"wrote {output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
