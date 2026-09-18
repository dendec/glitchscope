#!/bin/bash
# Run inside the Linux builder, where dependency sources and notices are present.
set -euo pipefail
out="$1"
mkdir -p "$out"
cp LICENSE "$out/GlitchScope-GPL-2.0-or-later.txt"
cp portmaster/licenses/* "$out/"
cp /opt/notices/* "$out/"
for entry in \
  'soloud:LICENSE' 'projectm:COPYING' 'game-music-emu:license.txt' \
  'game-music-emu:license.gpl2.txt' 'ayumi:LICENSE' 'pt3player:LICENSE' \
  'libstsound:COPYING' 'hvl:LICENSE.TXT' 'pixelarticons:LICENSE' \
  'cRSID:README.txt' 'cRSID:README.rockbox' 'ffmpeg:COPYING.LGPLv2.1'; do
  component="${entry%%:*}"
  notice="${entry#*:}"
  cp "lib/$component/$notice" "$out/$component-$notice"
done
cp lib/projectm/vendor/projectm-eval/LICENSE.md "$out/projectm-eval-LICENSE.md"
cp /usr/local/go/LICENSE "$out/Go-LICENSE"
module_dirs=$(go list -m -f '{{if not .Main}}{{.Dir}}{{end}}' all)
while IFS= read -r module_dir; do
  [ -n "$module_dir" ] || continue
  name="${module_dir##*/}"
  cp "$module_dir/LICENSE" "$out/go-$name-LICENSE"
done <<< "$module_dirs"
# Debian copyright files preserve notices for dynamically linked libraries and
# font tooling/assets. Include common license texts referenced by those files.
for package in libsdl2-2.0-0 libogg0 libvorbis0a libvorbisfile3 libflac12 \
  libmpg123-0 zlib1g libasound2 libstdc++6 libgcc-s1 libc6; do
  path="/usr/share/doc/$package/copyright"
  if [ ! -f "$path" ]; then path="/usr/share/doc/$package:arm64/copyright"; fi
  cp "$path" "$out/$package-copyright"
done
cp /usr/share/common-licenses/* "$out/"

python3 - "$out/Unifont-copyright.txt" <<'PYFONT'
from fontTools.ttLib import TTFont
import sys
font = TTFont('internal/ui/assets/unifont.otf')
notices = sorted({name.toUnicode() for name in font['name'].names if name.nameID == 0})
if not notices:
    raise SystemExit('Font copyright metadata is missing')
with open(sys.argv[1], 'w') as output:
    output.write('\n'.join(notices) + '\n')
PYFONT
