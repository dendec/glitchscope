# syntax=docker/dockerfile:1.7
ARG BUILDER_IMAGE=pmv-builder:latest
FROM ${BUILDER_IMAGE} AS builder

ARG TARGETARCH=amd64

WORKDIR /build

# Copy source files
COPY cmd/ cmd/
COPY internal/ internal/
COPY lib/ lib/
COPY test_data/ test_data/
COPY portmaster/ portmaster/

# Install pre-built font subset
RUN mkdir -p internal/ui/assets && cp /opt/font/unifont.otf internal/ui/assets/unifont.otf

# Build Go app for target architecture
RUN --mount=type=cache,target=/root/.cache/go-build \
    if [ "$TARGETARCH" = "arm64" ]; then \
        CGO_ENABLED=1 GOOS=linux GOARCH=arm64 \
        CC=aarch64-linux-gnu-gcc CXX=aarch64-linux-gnu-g++ \
        CGO_CFLAGS="-I/opt/xmp/arm64/include -I/opt/openmpt/arm64/include -I/opt/gme/arm64/include -I/opt/crsid/include" \
        CGO_CXXFLAGS="-std=c++11 -Wno-write-strings -DWITH_SDL2_STATIC -I/build/lib/soloud/include -I/opt/ayumi/include -I/opt/pt3player/include -I/opt/libstsound/include -I/opt/crsid/include -I/opt/ffmpeg/arm64/include -I/opt/projectm/include -I/opt/projectm/include/projectM-4 -I/opt/gme/arm64/include -I/opt/xmp/arm64/include -I/opt/openmpt/arm64/include -I/usr/include/SDL2 -D_REENTRANT" \
        CGO_LDFLAGS="-lSDL2 -lm -lpthread /opt/pt3player/arm64/lib/libpt3player.a /opt/ayumi/arm64/lib/libayumi.a /opt/libstsound/arm64/lib/libstsound.a /opt/crsid/arm64/lib/libcrsid.a /opt/ffmpeg/arm64/lib/libavformat.a /opt/ffmpeg/arm64/lib/libavcodec.a /opt/ffmpeg/arm64/lib/libswresample.a /opt/ffmpeg/arm64/lib/libavutil.a /opt/projectm/arm64/lib/libprojectM-4.a /opt/projectm/arm64/lib/libprojectM_eval.a -lGLESv2 -lm /opt/xmp/arm64/lib/libxmp.a /opt/openmpt/arm64/lib/libopenmpt.a /opt/gme/arm64/lib/libgme.a -lvorbisfile -lvorbis -lFLAC -logg -lmpg123 -lz -lstdc++" \
        go build -ldflags="-s -w" -o pmv ./cmd/pmv \
        && aarch64-linux-gnu-strip pmv \
        && (patchelf --remove-needed libGL.so.1 pmv 2>/dev/null || true); \
    else \
        CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
        CGO_CFLAGS="-I/opt/xmp/amd64/include -I/opt/openmpt/amd64/include -I/opt/crsid/include -I/opt/ffmpeg/amd64/include -I/usr/include/SDL2 -D_REENTRANT" \
        CGO_CXXFLAGS="-std=c++11 -Wno-write-strings -DWITH_SDL2_STATIC -I/build/lib/soloud/include -I/opt/ayumi/include -I/opt/pt3player/include -I/opt/libstsound/include -I/opt/crsid/include -I/opt/ffmpeg/amd64/include -I/opt/projectm/include -I/opt/projectm/include/projectM-4 -I/opt/gme/amd64/include -I/opt/xmp/amd64/include -I/opt/openmpt/amd64/include -I/usr/include/SDL2 -D_REENTRANT" \
        CGO_LDFLAGS="-lSDL2 /opt/pt3player/amd64/lib/libpt3player.a /opt/ayumi/amd64/lib/libayumi.a /opt/libstsound/amd64/lib/libstsound.a /opt/crsid/amd64/lib/libcrsid.a /opt/ffmpeg/amd64/lib/libavformat.a /opt/ffmpeg/amd64/lib/libavcodec.a /opt/ffmpeg/amd64/lib/libswresample.a /opt/ffmpeg/amd64/lib/libavutil.a /opt/projectm/amd64/lib/libprojectM-4.a /opt/projectm/amd64/lib/libprojectM_eval.a -lGL -lGLESv2 -lm -pthread /opt/xmp/amd64/lib/libxmp.a /opt/openmpt/amd64/lib/libopenmpt.a /opt/gme/amd64/lib/libgme.a -lvorbisfile -lvorbis -lFLAC -logg -lmpg123 -lz -lstdc++" \
        go build -ldflags="-s -w" -o pmv ./cmd/pmv; \
    fi

# Prepare output layout in /dist
RUN mkdir -p /dist/pmv/presets /dist/pmv/textures \
    && cp pmv /dist/pmv/ \
    && if [ "$TARGETARCH" = "arm64" ]; then \
        cp portmaster/PMV.sh /dist/ \
        && cp portmaster/port.json /dist/ \
        && cp portmaster/screenshot.png /dist/ 2>/dev/null; true \
        && mkdir -p /dist/pmv/licenses \
        && cp portmaster/LICENSE* /dist/pmv/licenses/ 2>/dev/null; true \
        && cp portmaster/README.md /dist/pmv/; \
    fi

# The image is an artifact carrier; Makefile targets extract /dist with docker cp.
FROM scratch AS runner
COPY --from=builder /dist /dist
ENTRYPOINT ["/dist/pmv/pmv"]
