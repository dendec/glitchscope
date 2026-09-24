#!/usr/bin/env python3
"""Translate the embedded Help JSON with a local Hy-MT2 llama.cpp model.

The script changes only ``title`` values and strings inside ``lines`` arrays.
IDs, tree structure, placeholders, blank lines, and license identifiers remain
untouched. Progress is written atomically after every translated string, so an
interrupted run can be resumed with the same output path.
"""

from __future__ import annotations

import argparse
import copy
import json
import os
import re
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Iterator


HF_HOME = Path(os.environ.get("HF_HOME") or Path.home() / ".cache" / "huggingface")
DEFAULT_MODEL = Path(
    os.environ.get("GLITCHSCOPE_TRANSLATION_MODEL")
    or HF_HOME
    / "hub"
    / "models--tencent--Hy-MT2-7B-GGUF"
    / "snapshots"
    / "ab8472660ac61fac25f1af43fac2599d52a8a775"
    / "Hy-MT2-7B-Q4_K_M.gguf"
)
DEFAULT_SERVER = Path(
    os.environ.get("GLITCHSCOPE_LLAMA_SERVER")
    or shutil.which("llama-server")
    or "llama-server"
)
DEFAULT_INPUT = Path("internal/ui/assets/help.json")

PLACEHOLDER_RE = re.compile(r"\{[a-z0-9_]+\}")
URL_RE = re.compile(r"https?://\S+")
FORMAT_RE = re.compile(r"\.[A-Za-z0-9]+")
PROTECTED_TERMS = (
    "GlitchScope",
    "Game Music Emu",
    "HivelyTracker",
    "libopenmpt",
    "libxmp",
    "pt3player",
    "StSound",
    "FFmpeg",
    "cRSID",
    "ayumi",
    "ModArchive",
    "Modland",
    "MilkDrop",
    "projectM",
)

# These values are references into assets/licenses and must not be translated.
LICENSE_NAMES = {
    "BSD-2-Clause",
    "BSD-3-Clause",
    "LGPL-2.1",
    "LGPL-2.1+",
    "MIT",
    "WTFPL",
    "zlib",
}

LANGUAGE_NAMES = {
    "bg": "Bulgarian",
    "de": "German",
    "en": "English",
    "es": "Spanish",
    "fr": "French",
    "id": "Indonesian",
    "it": "Italian",
    "ja": "Japanese",
    "ko": "Korean",
    "pl": "Polish",
    "pt": "Portuguese",
    "ro": "Romanian",
    "ru": "Russian",
    "tr": "Turkish",
    "uk": "Ukrainian",
    "zh-Hans": "Simplified Chinese",
    "zh-Hant": "Traditional Chinese",
    "pt-BR": "Brazilian Portuguese",
    "vi": "Vietnamese",
    "th": "Thai",
    "ms": "Malay",
}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--lang", required=True, help="target language code, for example ru")
    parser.add_argument("--input", type=Path, default=DEFAULT_INPUT)
    parser.add_argument("--output", type=Path, help="default: internal/i18n/assets/<lang>-help.json")
    parser.add_argument("--model", type=Path, default=DEFAULT_MODEL)
    parser.add_argument("--server", type=Path, default=DEFAULT_SERVER)
    parser.add_argument("--device", help="llama.cpp device name; omit for automatic selection")
    parser.add_argument("--ctx-size", type=int, default=4096)
    parser.add_argument("--timeout", type=float, default=180.0)
    parser.add_argument("--retries", type=int, default=3)
    parser.add_argument("--keep-server", action="store_true", help="do not stop a server started by this script")
    parser.add_argument("--base-url", help="use an already running OpenAI-compatible llama server")
    parser.add_argument("--fresh", action="store_true", help="ignore an existing output and translate everything again")
    return parser.parse_args()


def free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def start_server(args: argparse.Namespace) -> tuple[subprocess.Popen[bytes] | None, str]:
    if args.base_url:
        return None, args.base_url.rstrip("/")
    for path, label in ((args.server, "llama-server"), (args.model, "model")):
        if not path.is_file():
            raise FileNotFoundError(f"{label} not found: {path}")

    port = free_port()
    command = [
        str(args.server),
        "--model",
        str(args.model),
        "--host",
        "127.0.0.1",
        "--port",
        str(port),
        "--jinja",
        "--ctx-size",
        str(args.ctx_size),
        "--n-gpu-layers",
        "999",
    ]
    if args.device:
        command.extend(("--device", args.device))
    env = os.environ.copy()
    lib_dir = args.server.parent / "lib"
    env["LD_LIBRARY_PATH"] = str(lib_dir) + os.pathsep + env.get("LD_LIBRARY_PATH", "")
    process = subprocess.Popen(command, env=env)
    base_url = f"http://127.0.0.1:{port}"
    wait_until_ready(base_url, process, args.timeout)
    return process, base_url


def wait_until_ready(base_url: str, process: subprocess.Popen[bytes], timeout: float) -> None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise RuntimeError(f"llama-server exited with status {process.returncode}")
        try:
            with urllib.request.urlopen(base_url + "/health", timeout=2) as response:
                if response.status == 200:
                    return
        except (OSError, urllib.error.URLError):
            pass
        time.sleep(0.25)
    raise TimeoutError("timed out waiting for llama-server")


def translatable_paths(value: Any, path: tuple[Any, ...] = ()) -> Iterator[tuple[Any, ...]]:
    if isinstance(value, dict):
        title = value.get("title")
        if isinstance(title, str) and should_translate(title):
            yield path + ("title",)
        lines = value.get("lines")
        if isinstance(lines, list):
            for index, line in enumerate(lines):
                if isinstance(line, str) and should_translate(line):
                    yield path + ("lines", index)
        # Translate the section title above, but keep dependency names,
        # license identifiers, URLs, copyright notices, and embedded license
        # bodies byte-for-byte identical to the English asset.
        if value.get("id") == 9:
            return
        children = value.get("children")
        if isinstance(children, list):
            for index, child in enumerate(children):
                yield from translatable_paths(child, path + ("children", index))
    elif isinstance(value, list):
        for index, child in enumerate(value):
            yield from translatable_paths(child, path + (index,))


def should_translate(text: str) -> bool:
    return (
        bool(text.strip())
        and text not in LICENSE_NAMES
        and not URL_RE.fullmatch(text)
        and not FORMAT_RE.fullmatch(text)
    )


def get_path(root: Any, path: tuple[Any, ...]) -> Any:
    value = root
    for part in path:
        value = value[part]
    return value


def set_path(root: Any, path: tuple[Any, ...], value: str) -> None:
    parent = get_path(root, path[:-1])
    parent[path[-1]] = value


RUSSIAN_TERMINOLOGY = {
    "action hint": "подсказка действия",
    "footer": "нижняя панель",
    "preset": "пресет",
    "preset auto-switch": "автоматическая смена пресетов",
    "seeking": "перемотка",
    "shuffle": "перемешивание",
    "native streaming": "нативное потоковое декодирование",
    "streaming services": "стриминговые сервисы",
    "tracker module": "трекерный модуль",
    "visualizer": "визуализатор",
}


@dataclass(frozen=True)
class TranslationUnit:
    path: tuple[Any, ...]
    context: str


def translation_units(value: Any, path: tuple[Any, ...] = (), parents: tuple[str, ...] = ()) -> Iterator[TranslationUnit]:
    if isinstance(value, list):
        for index, child in enumerate(value):
            yield from translation_units(child, path + (index,), parents)
        return
    if not isinstance(value, dict):
        return
    title = value.get("title", "")
    context_parts = parents + ((title,) if title else ())
    has_title = isinstance(title, str) and should_translate(title)
    has_lines = any(isinstance(line, str) and should_translate(line) for line in value.get("lines", []))
    if has_title or has_lines:
        yield TranslationUnit(path, " > ".join(context_parts))
    if value.get("id") == 9:
        return
    for index, child in enumerate(value.get("children", [])):
        yield from translation_units(child, path + ("children", index), context_parts)


def protect_text(text: str, protected: dict[str, str]) -> str:
    patterns = [PLACEHOLDER_RE, URL_RE]
    result = text
    for pattern in patterns:
        for match in pattern.findall(text):
            token = f"__GS_TOKEN_{len(protected)}__"
            protected[token] = match
            result = result.replace(match, token)
    for term in PROTECTED_TERMS:
        if term in result:
            token = f"__GS_TOKEN_{len(protected)}__"
            protected[token] = term
            result = result.replace(term, token)
    if "**" in result:
        token = f"__GS_TOKEN_{len(protected)}__"
        protected[token] = "**"
        result = result.replace("**", token)
    return result


def restore_text(text: str, protected: dict[str, str]) -> str:
    result = text
    for token, original in protected.items():
        result = result.replace(token, original)
    leftovers = re.findall(r"__GS_TOKEN_\d+__", result)
    if leftovers:
        raise ValueError(f"unknown protected tokens: {leftovers}")
    return result


def unit_payload(node: dict[str, Any]) -> tuple[dict[str, Any], dict[str, str]]:
    protected: dict[str, str] = {}
    payload: dict[str, Any] = {}
    title = node.get("title")
    if isinstance(title, str):
        payload["title"] = protect_text(title, protected) if should_translate(title) else title
    if isinstance(node.get("lines"), list):
        payload["lines"] = [protect_text(line, protected) for line in node["lines"]]
    return payload, protected


def raw_unit_payload(node: dict[str, Any]) -> dict[str, Any]:
    payload: dict[str, Any] = {}
    if isinstance(node.get("title"), str):
        payload["title"] = node["title"]
    if isinstance(node.get("lines"), list):
        payload["lines"] = node["lines"]
    return payload


def structured_prompt(payload: dict[str, Any], context: str, target: str) -> str:
    glossary = ""
    if target == "Russian":
        translations = "\n".join(
            f"{source} translates to {translated}"
            for source, translated in RUSSIAN_TERMINOLOGY.items()
        )
        glossary = f"Reference the following translations:\n{translations}\n\n"
    source_data = json.dumps(payload, ensure_ascii=False, indent=2)
    return f"""{glossary}[Background Information]
This is concise user-facing Help for the GlitchScope music player. Current section: {context}.
Use natural, concise {target} UI language. Preserve product names, file formats, button labels, numbers, and technical abbreviations.

### Task
Translate the user-facing text values within the following JSON data into {target}.

### Strict Rules
1. Structure Preservation: You MUST preserve the original JSON structure, keys, array lengths, and ordering exactly.
2. Selective Translation: Translate ONLY the visible user-facing string values.
3. Strict Non-Translation: NEVER alter JSON keys or tokens matching __GS_TOKEN_N__. Leave those tokens exactly unchanged.
4. Output ONLY valid JSON without Markdown fences or additional explanation.

### Source Data
{source_data}"""


def contextual_prompt(text: str, context: str, target: str) -> str:
    glossary = ""
    if target == "Russian":
        translations = "\n".join(
            f"{source} translates to {translated}"
            for source, translated in RUSSIAN_TERMINOLOGY.items()
        )
        glossary = f"Reference the following translations:\n{translations}\n\n"
    return f"""{glossary}[Background Information]
This is concise user-facing Help for the GlitchScope music player. Current section: {context}.

Translate the following text into {target}. Note that you must ONLY output the translated result without any additional explanation:
{text}"""


def parse_json_response(text: str) -> dict[str, Any]:
    candidate = text.strip()
    if candidate.startswith("```"):
        lines = candidate.splitlines()
        candidate = "\n".join(lines[1:-1])
    value = json.loads(candidate)
    if not isinstance(value, dict):
        raise ValueError("model response is not a JSON object")
    return value


def translate_unit(args: argparse.Namespace, base_url: str, node: dict[str, Any], context: str, target: str) -> dict[str, Any]:
    payload, protected = unit_payload(node)
    last_error: Exception | None = None
    for attempt in range(1, args.retries + 1):
        try:
            response = request_prompt(
                base_url,
                structured_prompt(payload, context, target),
                args.timeout,
                2048,
                json_mode=True,
            )
            translated = parse_json_response(response)
            if translated.keys() != payload.keys():
                raise ValueError("JSON keys changed")
            if "lines" in payload and (
                not isinstance(translated.get("lines"), list)
                or len(translated["lines"]) != len(payload["lines"])
            ):
                raise ValueError("lines array shape changed")
            for key, value in translated.items():
                if key == "title":
                    translated[key] = restore_text(str(value), protected)
                else:
                    translated[key] = [restore_text(str(line), protected) for line in value]
            if "title" in translated:
                if not should_translate(str(node.get("title", ""))) and translated["title"] != node.get("title"):
                    raise ValueError("non-translatable title changed")
                validate_translation(str(node.get("title", "")), translated["title"])
            for source, result in zip(node.get("lines", []), translated.get("lines", [])):
                if not should_translate(source):
                    if source != result:
                        raise ValueError("non-translatable line changed")
                    continue
                validate_translation(source, result)
            return translated
        except (KeyError, OSError, ValueError, json.JSONDecodeError, urllib.error.URLError) as error:
            last_error = error
            print(f"retry {attempt}/{args.retries}: {error}", file=sys.stderr)
    print(f"structured translation failed for {context!r}; using contextual field fallback: {last_error}", file=sys.stderr)
    result: dict[str, Any] = {}
    title = node.get("title")
    if isinstance(title, str):
        result["title"] = translate_field(args, base_url, title, context, target) if should_translate(title) else title
    if isinstance(node.get("lines"), list):
        result["lines"] = [
            translate_field(args, base_url, line, context, target) if should_translate(line) else line
            for line in node["lines"]
        ]
    return result


def translate_field(args: argparse.Namespace, base_url: str, source: str, context: str, target: str) -> str:
    protected: dict[str, str] = {}
    safe_source = protect_text(source, protected)
    last_error: Exception | None = None
    for attempt in range(1, args.retries + 1):
        try:
            response = request_prompt(base_url, contextual_prompt(safe_source, context, target), args.timeout, 1024)
            translated = restore_text(response.strip(), protected)
            validate_translation(source, translated)
            return translated
        except (OSError, ValueError, urllib.error.URLError) as error:
            last_error = error
            print(f"field retry {attempt}/{args.retries}: {error}", file=sys.stderr)
    raise RuntimeError(f"could not translate field in {context!r}: {last_error}")


def request_prompt(
    base_url: str,
    prompt: str,
    timeout: float,
    max_tokens: int,
    *,
    json_mode: bool = False,
) -> str:
    options: dict[str, Any] = {
        "messages": [{"role": "user", "content": prompt}],
        "temperature": 0.7,
        "top_p": 0.6,
        "top_k": 20,
        "repetition_penalty": 1.05,
        "max_tokens": max_tokens,
    }
    if json_mode:
        options["response_format"] = {"type": "json_object"}
    payload = json.dumps(options).encode()
    request = urllib.request.Request(
        base_url + "/v1/chat/completions",
        data=payload,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=timeout) as response:
        result = json.load(response)
    return str(result["choices"][0]["message"]["content"]).strip()


def validate_translation(source: str, translated: str) -> None:
    if not translated:
        raise ValueError("empty translation")
    if PLACEHOLDER_RE.findall(source) != PLACEHOLDER_RE.findall(translated):
        raise ValueError("placeholder sequence changed")
    if source.count("**") != translated.count("**"):
        raise ValueError("Markdown marker count changed")
    for url in URL_RE.findall(source):
        if url not in translated:
            raise ValueError(f"URL changed: {url}")


def atomic_write(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    handle, temporary_name = tempfile.mkstemp(prefix=path.name + ".", dir=path.parent)
    try:
        with os.fdopen(handle, "w", encoding="utf-8") as stream:
            json.dump(value, stream, ensure_ascii=False, indent=2)
            stream.write("\n")
        os.chmod(temporary_name, 0o644)
        os.replace(temporary_name, path)
    except BaseException:
        Path(temporary_name).unlink(missing_ok=True)
        raise


def same_structure(source: Any, translated: Any) -> bool:
    if type(source) is not type(translated):
        return False
    if isinstance(source, dict):
        if source.keys() != translated.keys():
            return False
        return all(
            True if key in {"title", "lines"} else same_structure(source[key], translated[key])
            for key in source
        )
    if isinstance(source, list):
        return len(source) == len(translated) and all(
            same_structure(left, right) for left, right in zip(source, translated)
        )
    return source == translated


def main() -> int:
    args = parse_args()
    output = args.output or Path("internal/i18n/assets") / f"{args.lang}-help.json"
    target = LANGUAGE_NAMES.get(args.lang, args.lang)
    source = json.loads(args.input.read_text(encoding="utf-8"))
    if output.exists() and not args.fresh:
        translated = json.loads(output.read_text(encoding="utf-8"))
        if not same_structure(source, translated):
            raise ValueError(f"existing output has a different structure: {output}")
    else:
        translated = copy.deepcopy(source)

    units = list(translation_units(source))
    process: subprocess.Popen[bytes] | None = None
    try:
        process, base_url = start_server(args)
        for index, unit in enumerate(units, 1):
            source_node = get_path(source, unit.path)
            current_node = get_path(translated, unit.path)
            source_payload = raw_unit_payload(source_node)
            current_payload = {key: current_node.get(key) for key in source_payload}
            if current_payload != source_payload:
                print(f"[{index}/{len(units)}] resume {unit.context}")
                continue
            print(f"[{index}/{len(units)}] {unit.context}")
            result = translate_unit(args, base_url, source_node, unit.context, target)
            for key, value in result.items():
                current_node[key] = value
            atomic_write(output, translated)
        if not same_structure(source, translated):
            raise ValueError("translated output changed the Help tree structure")
        print(f"wrote {output}")
        return 0
    finally:
        if process is not None and not args.keep_server and process.poll() is None:
            process.send_signal(signal.SIGINT)
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()


if __name__ == "__main__":
    raise SystemExit(main())
