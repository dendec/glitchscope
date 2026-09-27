#!/usr/bin/env python3
"""Build the multilingual GitHub Pages site using only Python's standard library."""

import argparse
import html
import json
import shutil
from pathlib import Path
from string import Template
from urllib.parse import urlparse


ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "site"
OUTPUT = ROOT / "dist" / "site"
LANGUAGES = {"en": "EN", "ru": "RU", "zh-Hans": "中文"}
REPOSITORY = "https://github.com/dendec/glitchscope"


def escape(value):
    return html.escape(str(value), quote=True)


def shape(value):
    if isinstance(value, dict):
        return {key: shape(item) for key, item in value.items()}
    if isinstance(value, list):
        return [shape(item) for item in value]
    if not isinstance(value, str) or not value.strip():
        raise ValueError("Translations must contain nonempty strings")
    return "string"


def build(base_url):
    parsed = urlparse(base_url)
    if parsed.scheme not in ("http", "https") or not parsed.netloc:
        raise ValueError("--base-url must be an absolute HTTP(S) URL")
    base_url = base_url.rstrip("/") + "/"
    content = {
        lang: json.loads((SOURCE / "locales" / f"{lang}.json").read_text(encoding="utf-8"))
        for lang in LANGUAGES
    }
    for lang, data in content.items():
        if shape(data) != shape(content["en"]):
            raise ValueError(f"Translation structure differs from English: {lang}")
        if [p["id"] for p in data["platforms"]] != ["portmaster", "raspberry-pi", "linux", "windows"]:
            raise ValueError(f"Unexpected platform IDs: {lang}")

    OUTPUT.mkdir(parents=True, exist_ok=True)
    assets = OUTPUT / "assets"
    assets.mkdir(exist_ok=True)
    for name in ("styles.css", "site.js"):
        shutil.copyfile(SOURCE / name, assets / name)
    shutil.copyfile(ROOT / "internal" / "ui" / "app_icon.svg", assets / "glitchscope.svg")
    shutil.copyfile(ROOT / "internal" / "ui" / "app_icon.svg", assets / "favicon.svg")
    shutil.copyfile(ROOT / "portmaster" / "screenshot.png", assets / "screenshot.png")
    template = Template((SOURCE / "template.html").read_text(encoding="utf-8"))
    urls = {lang: base_url + ("" if lang == "en" else f"{lang}/") for lang in LANGUAGES}
    root_path = parsed.path.rstrip("/")
    for lang, data in content.items():
        prefix = "./" if lang == "en" else "../"
        variables = {key: escape(value) for key, value in data.items() if isinstance(value, str)}
        variables.update(lang=lang, prefix=prefix, repo=REPOSITORY, canonical=escape(urls[lang]))
        variables["language_redirect"] = ""
        if lang == "en":
            variables["language_redirect"] = """<script>
    (() => {
      const rootPath = __ROOT_PATH__;
      const currentPath = location.pathname.replace(/\\/+$/, "");
      const localPreview = (location.hostname === "localhost" || location.hostname === "127.0.0.1")
        && currentPath === "";
      if (currentPath !== rootPath && !localPreview) return;
      const siteRoot = localPreview ? "" : rootPath;
      const localeFor = (value) => {
        const language = value.toLowerCase().replaceAll("_", "-").split("-")[0];
        if (language === "zh") return "zh-Hans";
        return language === "en" || language === "ru" ? language : null;
      };
      let savedLanguage = null;
      try { savedLanguage = localeFor(localStorage.getItem("glitchscope-language") || ""); } catch {}
      const browserLanguage = [...(navigator.languages || []), navigator.language]
        .map(localeFor).find(Boolean);
      const locale = savedLanguage || browserLanguage || "en";
      if (locale !== "en") location.replace(`${siteRoot}/${locale}/${location.hash}`);
    })();
  </script>""".replace("__ROOT_PATH__", json.dumps(root_path))
        variables["alternates"] = "\n".join(
            f'<link rel="alternate" hreflang="{code}" href="{escape(url)}">'
            for code, url in {**urls, "x-default": urls["en"]}.items()
        )
        variables["languages"] = "".join(
            f'<a href="{prefix}{"" if code == "en" else code + "/"}" lang="{code}" '
            f'hreflang="{code}" {"aria-current=page" if code == lang else ""}>'
            f'{label}</a>' for code, label in LANGUAGES.items()
        )
        variables["features"] = "".join(
            f'<article class="feature"><span class="index">0{i}</span>'
            f'<h3>{escape(item["title"])}</h3><p>{escape(item["text"])}</p></article>'
            for i, item in enumerate(data["features"], 1)
        )
        variables["tabs"] = "".join(
            f'<button type="button" id="tab-{p["id"]}" role="tab" '
            f'aria-controls="{p["id"]}" aria-selected="{str(i == 0).lower()}" '
            f'tabindex="{0 if i == 0 else -1}">{escape(p["label"])}</button>'
            for i, p in enumerate(data["platforms"])
        )
        panels = []
        for p in data["platforms"]:
            steps = "".join(f'<li>{escape(step)}</li>' for step in p["steps"])
            command = ""
            if p["command"] != "none":
                command = (
                    '<div class="command"><div class="command-top"><span>TERMINAL</span>'
                    f'<button type="button" class="copy" hidden>{escape(data["copy"])}</button></div>'
                    f'<pre tabindex="0"><code>{escape(p["command"])}</code></pre></div>'
                )
            panels.append(
                f'<section class="platform-panel" id="{p["id"]}" aria-labelledby="heading-{p["id"]}">'
                f'<div class="platform-intro"><span class="tag">{escape(p["arch"])}</span>'
                f'<h3 id="heading-{p["id"]}">{escape(p["title"])}</h3>'
                f'<p>{escape(p["intro"])}</p><ol class="steps">{steps}</ol>'
                f'<a class="text-link" href="{REPOSITORY}/releases">{escape(data["release_files"])} ↗</a></div>'
                f'<div class="requirements"><h4>{escape(data["requirements"])}</h4><p>{escape(p["requires"])}</p>'
                f'<h4>{escape(data["bundled"])}</h4><p>{escape(p["bundled"])}</p>{command}'
                f'<p class="platform-note">{escape(p["note"])}</p></div></section>'
            )
        variables["panels"] = "".join(panels)
        variables["controls"] = "".join(
            f'<div><dt><kbd>{escape(c["key"])}</kbd></dt><dd>{escape(c["action"])}</dd></div>'
            for c in data["controls"]
        )
        destination = OUTPUT if lang == "en" else OUTPUT / lang
        destination.mkdir(exist_ok=True)
        (destination / "index.html").write_text(template.substitute(variables), encoding="utf-8")
    (OUTPUT / ".nojekyll").touch()
    sitemap = '<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">'
    sitemap += "".join(f"<url><loc>{escape(url)}</loc></url>" for url in urls.values()) + "</urlset>\n"
    (OUTPUT / "sitemap.xml").write_text(sitemap, encoding="utf-8")
    print(f"Built {len(LANGUAGES)} languages in {OUTPUT}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", default="https://dendec.github.io/glitchscope/")
    build(parser.parse_args().base_url)
