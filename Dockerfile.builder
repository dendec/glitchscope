FROM debian:bookworm-slim

# Install Go
RUN apt-get update && apt-get install -y --no-install-recommends wget ca-certificates \
    && wget -q https://go.dev/dl/go1.25.4.linux-amd64.tar.gz -O /tmp/go.tar.gz \
    && tar -C /usr/local -xzf /tmp/go.tar.gz && rm /tmp/go.tar.gz \
    && rm -rf /var/lib/apt/lists/*
ENV PATH=/usr/local/go/bin:$PATH

# Add arm64 arch & install tools & cross-compilers
RUN dpkg --add-architecture arm64 && apt-get update && apt-get install -y --no-install-recommends \
        build-essential cmake git patchelf \
        g++-aarch64-linux-gnu binutils-aarch64-linux-gnu \
        libsdl2-dev libsdl2-dev:arm64 \
        libgl1-mesa-dev libgles2-mesa-dev libgles2-mesa-dev:arm64 \
        libopenmpt-dev \
        python3-pip python3-fonttools \
        autoconf automake libtool \
    && pip3 install --break-system-packages fonttools \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build

# Copy local C/C++ libraries and patches from workspace
COPY lib/soloud lib/soloud
COPY lib/projectm lib/projectm
COPY lib/game-music-emu lib/game-music-emu
COPY lib/cRSID lib/cRSID
COPY patches patches

# Apply patch & prepare projectM
RUN cd lib/projectm \
    && (git apply /build/patches/projectm-feedback.patch 2>/dev/null || true) \
    && sed -i 's/#cmakedefine PROJECTM_VERSION_VCS @PROJECTM_VERSION_VCS@/#define PROJECTM_VERSION_VCS "Unknown"/' \
        config.h.cmake.in 2>/dev/null || true

# --- Build projectM (amd64) ---
RUN mkdir -p lib/projectm/build-amd64 && cd lib/projectm/build-amd64 && \
    cmake .. -DBUILD_SHARED_LIBS=OFF \
             -DENABLE_PLAYLIST=OFF \
             -DENABLE_SDL_UI=OFF \
             -DBUILD_TESTING=OFF \
             -DCMAKE_BUILD_TYPE=Release \
             -DENABLE_INSTALL=OFF \
             -DENABLE_GLES=OFF && \
    cmake --build . --target projectM -- -j$(nproc)

RUN mkdir -p /opt/projectm/amd64/lib \
    && cp lib/projectm/build-amd64/src/libprojectM/libprojectM-4.a /opt/projectm/amd64/lib/ \
    && cp lib/projectm/build-amd64/vendor/projectm-eval/projectm-eval/libprojectM_eval.a /opt/projectm/amd64/lib/

# --- Build projectM (arm64, cross-compiled) ---
RUN echo 'set(CMAKE_SYSTEM_NAME Linux)\n\
set(CMAKE_SYSTEM_PROCESSOR aarch64)\n\
set(CMAKE_C_COMPILER aarch64-linux-gnu-gcc)\n\
set(CMAKE_CXX_COMPILER aarch64-linux-gnu-g++)\n\
set(CMAKE_FIND_ROOT_PATH_MODE_PROGRAM NEVER)\n\
set(CMAKE_FIND_ROOT_PATH_MODE_LIBRARY BOTH)\n\
set(CMAKE_FIND_ROOT_PATH_MODE_INCLUDE BOTH)' > /build/toolchain-arm64.cmake

RUN mkdir -p lib/projectm/build-arm64 && cd lib/projectm/build-arm64 && \
    PKG_CONFIG_PATH=/usr/lib/aarch64-linux-gnu/pkgconfig \
    cmake .. -DCMAKE_TOOLCHAIN_FILE=/build/toolchain-arm64.cmake \
             -DBUILD_SHARED_LIBS=OFF \
             -DENABLE_PLAYLIST=OFF \
             -DENABLE_SDL_UI=OFF \
             -DBUILD_TESTING=OFF \
             -DCMAKE_BUILD_TYPE=Release \
             -DENABLE_INSTALL=OFF \
             -DENABLE_GLES=ON && \
    cmake --build . --target projectM -- -j$(nproc)

RUN mkdir -p /opt/projectm/arm64/lib \
    && cp lib/projectm/build-arm64/src/libprojectM/libprojectM-4.a /opt/projectm/arm64/lib/ \
    && cp lib/projectm/build-arm64/vendor/projectm-eval/projectm-eval/libprojectM_eval.a /opt/projectm/arm64/lib/

# --- Build libgme static for amd64 ---
RUN mkdir -p lib/game-music-emu/build-amd64 && cd lib/game-music-emu/build-amd64 && \
    cmake .. -DGME_BUILD_SHARED=OFF -DGME_BUILD_STATIC=ON \
             -DGME_BUILD_TESTING=OFF -DGME_BUILD_EXAMPLES=OFF \
             -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX=/opt/gme/amd64 && \
    cmake --build . --target gme_static -- -j$(nproc) && \
    cmake --install .

# --- Build libgme static for arm64 ---
RUN mkdir -p lib/game-music-emu/build-arm64 && cd lib/game-music-emu/build-arm64 && \
    PKG_CONFIG_PATH=/usr/lib/aarch64-linux-gnu/pkgconfig \
    cmake .. -DGME_BUILD_SHARED=OFF -DGME_BUILD_STATIC=ON \
             -DGME_BUILD_TESTING=OFF -DGME_BUILD_EXAMPLES=OFF \
             -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX=/opt/gme/arm64 \
             -DCMAKE_TOOLCHAIN_FILE=/build/toolchain-arm64.cmake && \
    cmake --build . --target gme_static -- -j$(nproc) && \
    cmake --install .

# Add arm64 dependencies for libopenmpt and libxmp
RUN apt-get update && apt-get install -y --no-install-recommends \
        libogg-dev libogg-dev:arm64 libvorbis-dev libvorbis-dev:arm64 \
        libflac-dev libflac-dev:arm64 libmpg123-dev libmpg123-dev:arm64 \
        zlib1g-dev zlib1g-dev:arm64 \
    && rm -rf /var/lib/apt/lists/*

# --- Build libopenmpt static for amd64 + arm64 ---
RUN wget -q https://lib.openmpt.org/files/libopenmpt/src/libopenmpt-0.8.7+release.autotools.tar.gz \
    && tar xzf libopenmpt-0.8.7+release.autotools.tar.gz \
    && rm libopenmpt-0.8.7+release.autotools.tar.gz \
    && cd libopenmpt-0.8.7+release.autotools \
    && ./configure --enable-static --disable-shared \
        --with-mpg123 --with-ogg --with-vorbis --with-vorbisfile --with-flac --with-zlib \
        --without-pulseaudio --without-sdl2 --without-portaudio --without-portaudiocpp --without-dsound \
        --without-sndfile \
        --prefix=/opt/openmpt/amd64 \
    && make -j$(nproc) \
    && make install \
    && make distclean \
    && PKG_CONFIG_PATH=/usr/lib/aarch64-linux-gnu/pkgconfig \
       ./configure --host=aarch64-linux-gnu --enable-static --disable-shared \
        --with-mpg123 --with-ogg --with-vorbis --with-vorbisfile --with-flac --with-zlib \
        --without-pulseaudio --without-sdl2 --without-portaudio --without-portaudiocpp --without-dsound \
        --without-sndfile \
        --prefix=/opt/openmpt/arm64 \
    && make -j$(nproc) \
    && make install \
    && rm -rf /build/libopenmpt-0.8.7+release.autotools

# --- Pre-build font subset ---
COPY internal/ui/font_ranges.json /tmp/font_ranges.json
RUN mkdir -p /opt/font \
    && wget -q https://unifoundry.com/pub/unifont/unifont-17.0.05/font-builds/unifont-17.0.05.otf -O /tmp/unifont-full.otf \
    && python3 -m fontTools.subset /tmp/unifont-full.otf \
        --unicodes=$(python3 -c "import json; print(','.join(json.load(open('/tmp/font_ranges.json'))))") \
        --no-subset-tables+=OS/2 \
        --no-prune-unicode-ranges \
        --output-file=/opt/font/unifont.otf 2>&1 \
    && rm -f /tmp/unifont-full.otf /tmp/font_ranges.json

# --- Build libxmp static for amd64 ---
RUN wget -q https://github.com/libxmp/libxmp/releases/download/libxmp-4.7.1/libxmp-4.7.1.tar.gz -O /tmp/libxmp-amd64.tar.gz \
    && tar xzf /tmp/libxmp-amd64.tar.gz \
    && rm /tmp/libxmp-amd64.tar.gz \
    && cd libxmp-4.7.1 \
    && ./configure --enable-static --disable-shared \
        --prefix=/opt/xmp/amd64 \
    && make -j$(nproc) \
    && make install \
    && rm -rf /build/libxmp-4.7.1

# --- Build libxmp static for arm64 ---
RUN wget -q https://github.com/libxmp/libxmp/releases/download/libxmp-4.7.1/libxmp-4.7.1.tar.gz -O /tmp/libxmp.tar.gz \
    && tar xzf /tmp/libxmp.tar.gz \
    && rm /tmp/libxmp.tar.gz \
    && cd libxmp-4.7.1 \
    && ./configure --host=aarch64-linux-gnu --enable-static --disable-shared \
        --prefix=/opt/xmp/arm64 \
    && make -j$(nproc) \
    && make install \
    && rm -rf /build/libxmp-4.7.1

# --- Build PT3 player core for amd64 and arm64 ---
COPY lib/pt3player lib/pt3player
COPY patches/pt3player-build.patch /build/patches/pt3player-build.patch
RUN cd lib/pt3player \
    && git apply /build/patches/pt3player-build.patch \
    && mkdir -p /opt/pt3player/amd64/lib /opt/pt3player/arm64/lib \
    && cc -std=c99 -O2 -Wno-unused-variable -Wno-parentheses -Wno-dangling-else -I. -c pt3player.c -o /tmp/pt3player-amd64.o \
    && ar rcs /opt/pt3player/amd64/lib/libpt3player.a /tmp/pt3player-amd64.o \
    && aarch64-linux-gnu-gcc -std=c99 -O2 -Wno-unused-variable -Wno-parentheses -Wno-dangling-else -I. -c pt3player.c -o /tmp/pt3player-arm64.o \
    && aarch64-linux-gnu-ar rcs /opt/pt3player/arm64/lib/libpt3player.a /tmp/pt3player-arm64.o \
    && mkdir -p /opt/pt3player/include \
    && cp pt3player.h /opt/pt3player/include/
# --- Build libayumi static for amd64 and arm64 ---
COPY lib/ayumi lib/ayumi
RUN mkdir -p /opt/ayumi/amd64/lib /opt/ayumi/arm64/lib \
    && cc -std=c99 -O2 -Ilib/ayumi -c lib/ayumi/ayumi.c -o /tmp/ayumi-amd64.o \
    && ar rcs /opt/ayumi/amd64/lib/libayumi.a /tmp/ayumi-amd64.o \
    && aarch64-linux-gnu-gcc -std=c99 -O2 -Ilib/ayumi -c lib/ayumi/ayumi.c -o /tmp/ayumi-arm64.o \
    && aarch64-linux-gnu-ar rcs /opt/ayumi/arm64/lib/libayumi.a /tmp/ayumi-arm64.o \
    && mkdir -p /opt/ayumi/include \
    && cp lib/ayumi/ayumi.h /opt/ayumi/include/

# --- Build libstsound static for amd64 and arm64 ---
COPY lib/libstsound lib/libstsound
RUN mkdir -p /opt/libstsound/amd64/lib /opt/libstsound/arm64/lib \
    && for source in YmMusic.cpp Ymload.cpp YmUserInterface.cpp Ym2149Ex.cpp digidrum.cpp YmFilters.cpp; do \
        g++ -std=c++11 -O2 -Wno-write-strings -Ilib/libstsound -Ilib/libstsound/LZH \
            -c lib/libstsound/$source -o /tmp/libstsound-amd64-${source%.cpp}.o; \
    done \
    && g++ -std=c++11 -O2 -Wno-write-strings -Ilib/libstsound -Ilib/libstsound/LZH \
        -c lib/libstsound/LZH/LzhLib.cpp -o /tmp/libstsound-amd64-LzhLib.o \
    && ar rcs /opt/libstsound/amd64/lib/libstsound.a /tmp/libstsound-amd64-*.o \
    && for source in YmMusic.cpp Ymload.cpp YmUserInterface.cpp Ym2149Ex.cpp digidrum.cpp YmFilters.cpp; do \
        aarch64-linux-gnu-g++ -std=c++11 -O2 -Wno-write-strings -Ilib/libstsound -Ilib/libstsound/LZH \
            -c lib/libstsound/$source -o /tmp/libstsound-arm64-${source%.cpp}.o; \
    done \
    && aarch64-linux-gnu-g++ -std=c++11 -O2 -Wno-write-strings -Ilib/libstsound -Ilib/libstsound/LZH \
        -c lib/libstsound/LZH/LzhLib.cpp -o /tmp/libstsound-arm64-LzhLib.o \
    && aarch64-linux-gnu-ar rcs /opt/libstsound/arm64/lib/libstsound.a /tmp/libstsound-arm64-*.o \
    && mkdir -p /opt/libstsound/include \
    && cp lib/libstsound/StSoundLibrary.h lib/libstsound/YmTypes.h /opt/libstsound/include/

# --- Build cRSID static for amd64 and arm64 ---
RUN mkdir -p /opt/crsid/amd64/lib /opt/crsid/arm64/lib \
    && cc -std=c99 -O2 -Ilib/cRSID -c lib/cRSID/libcRSID.c -o /tmp/libcrsid-amd64.o \
    && ar rcs /opt/crsid/amd64/lib/libcrsid.a /tmp/libcrsid-amd64.o \
    && aarch64-linux-gnu-gcc -std=c99 -O2 -Ilib/cRSID -c lib/cRSID/libcRSID.c -o /tmp/libcrsid-arm64.o \
    && aarch64-linux-gnu-ar rcs /opt/crsid/arm64/lib/libcrsid.a /tmp/libcrsid-arm64.o \
    && mkdir -p /opt/crsid/include \
    && cp lib/cRSID/libcRSID.h /opt/crsid/include/

# Pre-download Go dependencies
COPY go.mod go.sum* ./
RUN go mod download

# Install golangci-lint using container Go toolchain
RUN GOBIN=/usr/local/bin go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Copy SoLoud headers
RUN mkdir -p /opt/soloud/include && cp -r lib/soloud/include/* /opt/soloud/include/

# Export headers and place generated export header into source include tree
RUN cp lib/projectm/build-amd64/src/api/include/projectM-4/projectM_export.h lib/projectm/src/api/include/projectM-4/ \
    && cp lib/projectm/build-amd64/src/api/include/projectM-4/version.h lib/projectm/src/api/include/projectM-4/ \
    && mkdir -p /opt/projectm/include \
    && cp -r lib/projectm/src/api/include/* /opt/projectm/include/
