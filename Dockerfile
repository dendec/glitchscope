ARG BUILDER_IMAGE=mdpp-builder:latest
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
RUN if [ "$TARGETARCH" = "arm64" ]; then \
        CGO_ENABLED=1 GOOS=linux GOARCH=arm64 \
        CC=aarch64-linux-gnu-gcc CXX=aarch64-linux-gnu-g++ \
        CGO_CFLAGS="-I/opt/xmp/arm64/include -I/opt/openmpt/arm64/include" \
        CGO_CXXFLAGS="-std=c++11 -Wno-write-strings -DWITH_SDL2_STATIC -I/build/lib/soloud/include -I/opt/projectm/include -I/opt/projectm/include/projectM-4 -I/opt/xmp/arm64/include -I/opt/openmpt/arm64/include -I/usr/include/SDL2 -D_REENTRANT" \
        CGO_LDFLAGS="-lSDL2 -lm -lpthread /opt/projectm/arm64/lib/libprojectM-4.a /opt/projectm/arm64/lib/libprojectM_eval.a -lGLESv2 -lm /opt/xmp/arm64/lib/libxmp.a /opt/openmpt/arm64/lib/libopenmpt.a -lstdc++" \
        go build -ldflags="-s -w" -o mdpp ./cmd/mdpp \
        && aarch64-linux-gnu-strip mdpp \
        && (patchelf --remove-needed libGL.so.1 mdpp 2>/dev/null || true); \
    else \
        CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
        CGO_CFLAGS="-I/opt/xmp/amd64/include -I/opt/openmpt/amd64/include -I/usr/include/SDL2 -D_REENTRANT" \
        CGO_CXXFLAGS="-std=c++11 -Wno-write-strings -DWITH_SDL2_STATIC -I/build/lib/soloud/include -I/opt/projectm/include -I/opt/projectm/include/projectM-4 -I/opt/xmp/amd64/include -I/opt/openmpt/amd64/include -I/usr/include/SDL2 -D_REENTRANT" \
        CGO_LDFLAGS="-lSDL2 /opt/projectm/amd64/lib/libprojectM-4.a /opt/projectm/amd64/lib/libprojectM_eval.a -lGL -lGLESv2 -lm /opt/xmp/amd64/lib/libxmp.a /opt/openmpt/amd64/lib/libopenmpt.a -lstdc++" \
        go build -ldflags="-s -w" -o mdpp ./cmd/mdpp; \
    fi

# Prepare output layout in /dist
RUN mkdir -p /dist/mdpp/presets /dist/mdpp/textures \
    && cp mdpp /dist/mdpp/ \
    && if [ "$TARGETARCH" = "arm64" ]; then \
        cp portmaster/MDPP.sh /dist/ \
        && cp portmaster/port.json /dist/ \
        && cp portmaster/screenshot.png /dist/ 2>/dev/null; true \
        && mkdir -p /dist/mdpp/licenses \
        && cp portmaster/LICENSE* /dist/mdpp/licenses/ 2>/dev/null; true \
        && cp portmaster/README.md /dist/mdpp/; \
    fi

# Minimal runtime stage
FROM debian:bookworm-slim AS runner

WORKDIR /dist
COPY --from=builder /dist /dist

ENTRYPOINT ["/dist/mdpp/mdpp"]
