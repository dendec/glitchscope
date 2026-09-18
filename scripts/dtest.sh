#!/usr/bin/env bash
# dtest.sh — run Go checks inside the glitchscope-builder image WITHOUT rebuilding it.
#
# `make test` / `make lint` first run `make builder`, which rebuilds the image.
# When the builder image already exists (and rebuilds are slow or blocked), use
# this helper to run the same checks directly against the existing image.
#
# Usage:
#   scripts/dtest.sh test     # go test -count=1 ./cmd/... ./internal/...
#   scripts/dtest.sh lint     # golangci-lint run ./cmd/... ./internal/...
set -euo pipefail

IMAGE="${DOCKER_BUILDER:-glitchscope-builder:latest}"
GO_CACHE_VOL="${DOCKER_GO_CACHE:-glitchscope-go-build-cache}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Env vars are passed *inside* the container (as the Makefile's DOCKER_GO_ENV
# does): `docker run` does not forward the client process env to the container,
# so the whole assignment string is fed to `bash -c` and parsed there.
GO_ENV='GOFLAGS=-buildvcs=false CGO_ENABLED=1 CGO_CFLAGS="-I/opt/xmp/amd64/include -I/opt/openmpt/amd64/include -I/opt/crsid/include -I/opt/ffmpeg/amd64/include -I/usr/include/SDL2 -D_REENTRANT" CGO_CXXFLAGS="-std=c++11 -Wno-write-strings -DWITH_SDL2_STATIC -I/opt/soloud/include -I/opt/ayumi/include -I/opt/pt3player/include -I/opt/libstsound/include -I/opt/crsid/include -I/opt/ffmpeg/amd64/include -I/opt/projectm/include -I/opt/projectm/include/projectM-4 -I/opt/gme/amd64/include -I/opt/xmp/amd64/include -I/opt/openmpt/amd64/include -I/usr/include/SDL2 -D_REENTRANT" CGO_LDFLAGS="-lSDL2 /opt/pt3player/amd64/lib/libpt3player.a /opt/ayumi/amd64/lib/libayumi.a /opt/libstsound/amd64/lib/libstsound.a /opt/crsid/amd64/lib/libcrsid.a /opt/ffmpeg/amd64/lib/libavformat.a /opt/ffmpeg/amd64/lib/libavcodec.a /opt/ffmpeg/amd64/lib/libswresample.a /opt/ffmpeg/amd64/lib/libavutil.a /opt/projectm/amd64/lib/libprojectM-4.a /opt/projectm/amd64/lib/libprojectM_eval.a -lGL -lGLESv2 -lm -pthread /opt/xmp/amd64/lib/libxmp.a /opt/openmpt/amd64/lib/libopenmpt.a /opt/gme/amd64/lib/libgme.a -lvorbisfile -lvorbis -lFLAC -logg -lmpg123 -lz -lstdc++"'

sub="$1"
shift

run_in_env() {
	local cmd="$1"
	docker run --rm -e GLITCHSCOPE_RADIO_TEST_URL -v "$ROOT:/build" -v "$GO_CACHE_VOL:/root/.cache/go-build" -w /build "$IMAGE" bash -c "$GO_ENV $cmd"
}

case "$sub" in
test)
	run_in_env 'python3 -m unittest discover -s scripts -p test_package_portmaster.py'
	run_in_env 'go test -count=1 ./cmd/... ./internal/...'
	;;
race)
	run_in_env 'go test -race -count=1 ./internal/player ./internal/radio ./internal/util ./internal/ui ./internal/app ./internal/modarchive'
	;;
native-radio)
	docker run --rm -v "$ROOT:/build" -w /build "$IMAGE" bash -c 'g++ -std=c++11 -DWITH_NULL -g -fsanitize=address,undefined -fno-omit-frame-pointer -Ilib/soloud/include -I/opt/ffmpeg/amd64/include scripts/test-radio-native.cpp internal/soloud/stream_source.cpp lib/soloud/src/core/*.cpp lib/soloud/src/backend/null/soloud_null.cpp /opt/ffmpeg/amd64/lib/libavformat.a /opt/ffmpeg/amd64/lib/libavcodec.a /opt/ffmpeg/amd64/lib/libswresample.a /opt/ffmpeg/amd64/lib/libavutil.a -lpthread -lm -o /tmp/test-radio-native && /tmp/test-radio-native'
	;;
lint)
	run_in_env 'golangci-lint run --timeout=5m ./cmd/... ./internal/...'
	;;
*)
	echo "usage: $0 {test|lint|race|native-radio}" >&2
	exit 2
	;;
esac
