# syntax=docker/dockerfile:1.7
ARG BUILDER_IMAGE=glitchscope-builder:latest
FROM ${BUILDER_IMAGE} AS builder

ARG TARGETARCH=amd64
ARG APP_VERSION=1.0

WORKDIR /build

# Copy source files
COPY cmd/ cmd/
COPY internal/ internal/
COPY lib/ lib/
COPY test_data/ test_data/
COPY portmaster/ portmaster/
COPY scripts/ scripts/
COPY LICENSE LICENSE
COPY patches/soloud-xmp.patch patches/soloud-xmp.patch

RUN if git apply --check patches/soloud-xmp.patch >/dev/null 2>&1; then \
        git apply patches/soloud-xmp.patch; \
    elif git apply --reverse --check patches/soloud-xmp.patch >/dev/null 2>&1; then \
        :; \
    else \
        echo 'soloud-xmp.patch does not apply' >&2; exit 1; \
    fi

# Use the generated font subset copied with the source tree.  Do not replace it
# with the builder image's cached font: the subset depends on the current locale
# assets and internal/ui/font_ranges.json.
RUN test -s internal/ui/assets/unifont.otf

# Rasterize the selected Pixelarticons into the binary assets used by the UI.
RUN python3 scripts/generate-icons.py \
    --manifest internal/ui/icon_assets.json \
    --output internal/ui/assets/icons

# Build Go app for target architecture
RUN --mount=type=cache,target=/root/.cache/go-build \
    if [ "$TARGETARCH" = "arm64" ]; then \
        CGO_ENABLED=1 GOOS=linux GOARCH=arm64 \
        CC=aarch64-linux-gnu-gcc CXX=aarch64-linux-gnu-g++ \
        CGO_CFLAGS="-I/opt/xmp/arm64/include -I/opt/openmpt/arm64/include -I/opt/gme/arm64/include -I/opt/crsid/include" \
        CGO_CXXFLAGS="-std=c++11 -Wno-write-strings -DWITH_SDL2_STATIC -I/build/lib/soloud/include -I/opt/ayumi/include -I/opt/pt3player/include -I/opt/libstsound/include -I/opt/crsid/include -I/opt/ffmpeg/arm64/include -I/opt/projectm/include -I/opt/projectm/include/projectM-4 -I/opt/gme/arm64/include -I/opt/xmp/arm64/include -I/opt/openmpt/arm64/include -I/usr/include/SDL2 -D_REENTRANT" \
        CGO_LDFLAGS="-lSDL2 -lm -lpthread /opt/pt3player/arm64/lib/libpt3player.a /opt/ayumi/arm64/lib/libayumi.a /opt/libstsound/arm64/lib/libstsound.a /opt/crsid/arm64/lib/libcrsid.a /opt/ffmpeg/arm64/lib/libavformat.a /opt/ffmpeg/arm64/lib/libavcodec.a /opt/ffmpeg/arm64/lib/libswresample.a /opt/ffmpeg/arm64/lib/libavutil.a /opt/projectm/arm64/lib/libprojectM-4.a /opt/projectm/arm64/lib/libprojectM_eval.a -lGLESv2 -lm /opt/xmp/arm64/lib/libxmp.a /opt/openmpt/arm64/lib/libopenmpt.a /opt/gme/arm64/lib/libgme.a -lvorbisfile -lvorbis -lFLAC -logg -lmpg123 -lz -lstdc++" \
        go build -ldflags="-s -w -X github.com/dendec/glitchscope/internal/version.Version=${APP_VERSION}" -o glitchscope ./cmd/glitchscope \
        && aarch64-linux-gnu-strip glitchscope \
        && (patchelf --remove-needed libGL.so.1 glitchscope 2>/dev/null || true); \
    else \
        CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
        CGO_CFLAGS="-I/opt/xmp/amd64/include -I/opt/openmpt/amd64/include -I/opt/crsid/include -I/opt/ffmpeg/amd64/include -I/usr/include/SDL2 -D_REENTRANT" \
        CGO_CXXFLAGS="-std=c++11 -Wno-write-strings -DWITH_SDL2_STATIC -I/build/lib/soloud/include -I/opt/ayumi/include -I/opt/pt3player/include -I/opt/libstsound/include -I/opt/crsid/include -I/opt/ffmpeg/amd64/include -I/opt/projectm/include -I/opt/projectm/include/projectM-4 -I/opt/gme/amd64/include -I/opt/xmp/amd64/include -I/opt/openmpt/amd64/include -I/usr/include/SDL2 -D_REENTRANT" \
        CGO_LDFLAGS="-lSDL2 /opt/pt3player/amd64/lib/libpt3player.a /opt/ayumi/amd64/lib/libayumi.a /opt/libstsound/amd64/lib/libstsound.a /opt/crsid/amd64/lib/libcrsid.a /opt/ffmpeg/amd64/lib/libavformat.a /opt/ffmpeg/amd64/lib/libavcodec.a /opt/ffmpeg/amd64/lib/libswresample.a /opt/ffmpeg/amd64/lib/libavutil.a /opt/projectm/amd64/lib/libprojectM-4.a /opt/projectm/amd64/lib/libprojectM_eval.a -lGL -lGLESv2 -lm -pthread /opt/xmp/amd64/lib/libxmp.a /opt/openmpt/amd64/lib/libopenmpt.a /opt/gme/amd64/lib/libgme.a -lvorbisfile -lvorbis -lFLAC -logg -lmpg123 -lz -lstdc++" \
        go build -ldflags="-s -w -X github.com/dendec/glitchscope/internal/version.Version=${APP_VERSION}" -o glitchscope ./cmd/glitchscope; \
    fi

# Prepare output layout in /dist
RUN mkdir -p /dist/glitchscope/presets /dist/glitchscope/textures \
    && cp glitchscope /dist/glitchscope/ \
    && if [ "$TARGETARCH" = "arm64" ]; then \
        cp portmaster/GlitchScope.sh /dist/ \
        && cp portmaster/port.json /dist/ \
        && cp portmaster/screenshot.png /dist/ \
        && mkdir -p /dist/glitchscope/libs.aarch64 \
        && for library in libvorbisfile.so.3 libvorbis.so.0 libogg.so.0 libFLAC.so.12 libmpg123.so.0 libz.so.1 libstdc++.so.6 libgcc_s.so.1; do \
            cp -L "/usr/lib/aarch64-linux-gnu/$library" /dist/glitchscope/libs.aarch64/ || exit 1; \
        done \
        && bash scripts/collect-portmaster-licenses.sh /dist/glitchscope/licenses \
        && cp portmaster/README.md /dist/glitchscope/; \
    fi

# The image is an artifact carrier; Makefile targets extract /dist with docker cp.
FROM scratch AS runner
COPY --from=builder /dist /dist
ENTRYPOINT ["/dist/glitchscope/glitchscope"]
