# x86_64 dev/build Docker image.
# Build:  docker build -t mdpp-builder .
# Run:    docker run --rm -it --net=host -e DISPLAY -v /tmp/.X11-unix:/tmp/.X11-unix mdpp-builder
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends wget ca-certificates \
    && wget -q https://go.dev/dl/go1.25.4.linux-amd64.tar.gz -O /tmp/go.tar.gz \
    && tar -C /usr/local -xzf /tmp/go.tar.gz && rm /tmp/go.tar.gz \
    && rm -rf /var/lib/apt/lists/*
ENV PATH=/usr/local/go/bin:$PATH

RUN apt-get update && apt-get install -y --no-install-recommends \
        build-essential cmake git \
        libsdl2-dev \
        libgl1-mesa-dev libgles2-mesa-dev \
        libopenmpt-dev \
        python3-pip \
    && pip3 install --break-system-packages fonttools \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build

# Clone SoLoud
RUN git clone --depth 1 https://github.com/jarikomppa/soloud.git lib/soloud

# Clone & patch projectM
RUN git clone --depth 1 --branch v4.1.6 \
    https://github.com/projectM-visualizer/projectm.git lib/projectm \
    && cd lib/projectm && git submodule update --init --recursive
COPY patches/ patches/
RUN cd lib/projectm \
    && git apply /build/patches/projectm-feedback.patch \
    && sed -i 's/#cmakedefine PROJECTM_VERSION_VCS @PROJECTM_VERSION_VCS@/#define PROJECTM_VERSION_VCS "Unknown"/' \
        config.h.cmake.in \
    && grep -q "bind_feedback_framebuffer" src/api/include/projectM-4/render_opengl.h \
    && echo "Patch verified"

# Build projectM static lib (GLES=OFF for x86_64 desktop)
RUN mkdir -p lib/projectm/build && cd lib/projectm/build && \
    cmake .. -DBUILD_SHARED_LIBS=OFF \
             -DENABLE_PLAYLIST=OFF \
             -DENABLE_SDL_UI=OFF \
             -DBUILD_TESTING=OFF \
             -DCMAKE_BUILD_TYPE=Release \
             -DENABLE_INSTALL=OFF \
             -DENABLE_GLES=OFF && \
    cmake --build . --target projectM -- -j$(nproc)

# Download Go deps
COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true

# Subset the font before copying application source so this network-bound
# layer is reused while only Go code changes.
COPY internal/ui/font_ranges.json internal/ui/font_ranges.json
RUN mkdir -p internal/ui/assets \
    && wget -q https://unifoundry.com/pub/unifont/unifont-17.0.05/font-builds/unifont-17.0.05.otf -O /tmp/unifont-full.otf \
    && pyftsubset /tmp/unifont-full.otf \
        --unicodes=$(python3 -c "import json; print(','.join(json.load(open('internal/ui/font_ranges.json'))))") \
        --no-subset-tables+=OS/2 \
        --output-file=internal/ui/assets/unifont.otf 2>&1 \
    && rm -f /tmp/unifont-full.otf

# Copy source
COPY cmd/ cmd/
COPY internal/ internal/
COPY test_data/ test_data/

# Build Go app
RUN CGO_ENABLED=1 \
    CGO_CFLAGS="-I/usr/include/SDL2 -D_REENTRANT" \
    CGO_CXXFLAGS="-std=c++11 -Wno-write-strings -DWITH_SDL2_STATIC -I lib/soloud/include -I lib/projectm/src/api/include -I lib/projectm/build/src/api/include -I/usr/include/SDL2 -D_REENTRANT" \
    CGO_LDFLAGS="-lSDL2 lib/projectm/build/src/libprojectM/libprojectM-4.a lib/projectm/build/vendor/projectm-eval/projectm-eval/libprojectM_eval.a -lGL -lGLESv2 -lm -lopenmpt" \
    go build -ldflags="-s -w" -o mdpp ./cmd/mdpp

# Prepare dist (clone textures here — only needed for packaging, not build)
RUN git clone --depth 1 https://github.com/projectM-visualizer/presets-milkdrop-texture-pack.git /tmp/textures \
    && mkdir -p /dist/mdpp/presets /dist/mdpp/textures \
    && cp mdpp /dist/mdpp/ \
    && cp lib/projectm/presets/tests/*.milk /dist/mdpp/presets/ \
    && cp /tmp/textures/textures/* /dist/mdpp/textures/ \
    && rm -f /dist/mdpp/presets/999-empty.milk /tmp/textures 2>/dev/null; true

ENTRYPOINT ["/dist/mdpp/mdpp"]
