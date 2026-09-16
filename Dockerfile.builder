FROM debian:bookworm-slim

ARG GO_VERSION=1.25.4
ARG OPENMPT_VERSION=0.8.7
ARG XMP_VERSION=4.7.1
ARG UNIFONT_VERSION=17.0.05

# Add arm64 architecture and install the complete build toolchain.
RUN dpkg --add-architecture arm64 \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
        wget ca-certificates \
        build-essential cmake git patchelf \
        g++-aarch64-linux-gnu binutils-aarch64-linux-gnu \
        libsdl2-dev libsdl2-dev:arm64 \
        libgl1-mesa-dev libgles2-mesa-dev libgles2-mesa-dev:arm64 \
        libopenmpt-dev \
        libogg-dev libogg-dev:arm64 \
        libvorbis-dev libvorbis-dev:arm64 \
        libflac-dev libflac-dev:arm64 \
        libmpg123-dev libmpg123-dev:arm64 \
        zlib1g-dev zlib1g-dev:arm64 \
        python3-fonttools \
        python3-pil librsvg2-bin \
        autoconf automake libtool \
    && rm -rf /var/lib/apt/lists/*

# Install the Go toolchain separately so changing GO_VERSION does not rerun apt.
RUN wget -q https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz -O /tmp/go.tar.gz \
    && tar -C /usr/local -xzf /tmp/go.tar.gz \
    && rm /tmp/go.tar.gz
ENV PATH=/usr/local/go/bin:$PATH

WORKDIR /build

# Apply patch & prepare projectM
COPY lib/projectm lib/projectm
COPY patches/projectm-feedback.patch /build/patches/projectm-feedback.patch
COPY patches/projectm-beat-sensitivity.patch /build/patches/projectm-beat-sensitivity.patch
COPY patches/projectm-transition-shaders.patch /build/patches/projectm-transition-shaders.patch
COPY patches/projectm-transition-framebuffer.patch /build/patches/projectm-transition-framebuffer.patch
COPY patches/projectm-transition-filter.patch /build/patches/projectm-transition-filter.patch
RUN cd lib/projectm \
    && if git apply --check /build/patches/projectm-feedback.patch >/dev/null 2>&1; then \
           git apply /build/patches/projectm-feedback.patch; \
    elif git apply --reverse --check /build/patches/projectm-feedback.patch >/dev/null 2>&1; then \
           :; \
       else \
           echo 'projectm-feedback.patch does not apply' >&2; exit 1; \
       fi \
    && if git apply --check /build/patches/projectm-beat-sensitivity.patch >/dev/null 2>&1; then \
           git apply /build/patches/projectm-beat-sensitivity.patch; \
    elif git apply --reverse --check /build/patches/projectm-beat-sensitivity.patch >/dev/null 2>&1; then \
           :; \
       else \
           echo 'projectm-beat-sensitivity.patch does not apply' >&2; exit 1; \
       fi \
    && if git apply --check /build/patches/projectm-transition-shaders.patch >/dev/null 2>&1; then \
           git apply /build/patches/projectm-transition-shaders.patch; \
       elif git apply --reverse --check /build/patches/projectm-transition-shaders.patch >/dev/null 2>&1; then \
           :; \
       else \
           echo 'projectm-transition-shaders.patch does not apply' >&2; exit 1; \
       fi \
    && if git apply --check /build/patches/projectm-transition-framebuffer.patch >/dev/null 2>&1; then \
           git apply /build/patches/projectm-transition-framebuffer.patch; \
       elif git apply --reverse --check /build/patches/projectm-transition-framebuffer.patch >/dev/null 2>&1; then \
           :; \
       else \
           echo 'projectm-transition-framebuffer.patch does not apply' >&2; exit 1; \
       fi \
    && if git apply --check /build/patches/projectm-transition-filter.patch >/dev/null 2>&1; then \
           git apply /build/patches/projectm-transition-filter.patch; \
       elif git apply --reverse --check /build/patches/projectm-transition-filter.patch >/dev/null 2>&1; then \
           :; \
       else \
           echo 'projectm-transition-filter.patch does not apply' >&2; exit 1; \
       fi \
    && sed -i 's/#cmakedefine PROJECTM_VERSION_VCS @PROJECTM_VERSION_VCS@/#define PROJECTM_VERSION_VCS "Unknown"/' \
        config.h.cmake.in

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
    && cp lib/projectm/build-amd64/vendor/projectm-eval/projectm-eval/libprojectM_eval.a /opt/projectm/amd64/lib/ \
    && nm -C /opt/projectm/amd64/lib/libprojectM-4.a | grep -E ' T libprojectM::ProjectM::BindFeedbackFramebuffer\(\)'

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
COPY lib/game-music-emu lib/game-music-emu
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

# --- Build libopenmpt static for amd64 + arm64 ---
RUN wget -q https://lib.openmpt.org/files/libopenmpt/src/libopenmpt-${OPENMPT_VERSION}+release.autotools.tar.gz \
    && tar xzf libopenmpt-${OPENMPT_VERSION}+release.autotools.tar.gz \
    && rm libopenmpt-${OPENMPT_VERSION}+release.autotools.tar.gz \
    && cd libopenmpt-${OPENMPT_VERSION}+release.autotools \
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
    && rm -rf /build/libopenmpt-${OPENMPT_VERSION}+release.autotools

# --- Build libxmp static for amd64 ---
RUN wget -q https://github.com/libxmp/libxmp/releases/download/libxmp-${XMP_VERSION}/libxmp-${XMP_VERSION}.tar.gz -O /tmp/libxmp-amd64.tar.gz \
    && tar xzf /tmp/libxmp-amd64.tar.gz \
    && rm /tmp/libxmp-amd64.tar.gz \
    && cd libxmp-${XMP_VERSION} \
    && ./configure --enable-static --disable-shared \
        --prefix=/opt/xmp/amd64 \
    && make -j$(nproc) \
    && make install \
    && rm -rf /build/libxmp-${XMP_VERSION}

# --- Build libxmp static for arm64 ---
RUN wget -q https://github.com/libxmp/libxmp/releases/download/libxmp-${XMP_VERSION}/libxmp-${XMP_VERSION}.tar.gz -O /tmp/libxmp.tar.gz \
    && tar xzf /tmp/libxmp.tar.gz \
    && rm /tmp/libxmp.tar.gz \
    && cd libxmp-${XMP_VERSION} \
    && ./configure --host=aarch64-linux-gnu --enable-static --disable-shared \
        --prefix=/opt/xmp/arm64 \
    && make -j$(nproc) \
    && make install \
    && rm -rf /build/libxmp-${XMP_VERSION}

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
COPY lib/cRSID lib/cRSID
RUN mkdir -p /opt/crsid/amd64/lib /opt/crsid/arm64/lib \
    && cc -std=c99 -O2 -Ilib/cRSID -c lib/cRSID/libcRSID.c -o /tmp/libcrsid-amd64.o \
    && ar rcs /opt/crsid/amd64/lib/libcrsid.a /tmp/libcrsid-amd64.o \
    && aarch64-linux-gnu-gcc -std=c99 -O2 -Ilib/cRSID -c lib/cRSID/libcRSID.c -o /tmp/libcrsid-arm64.o \
    && aarch64-linux-gnu-ar rcs /opt/crsid/arm64/lib/libcrsid.a /tmp/libcrsid-arm64.o \
    && mkdir -p /opt/crsid/include \
    && cp lib/cRSID/libcRSID.h /opt/crsid/include/

# --- Build the audio-only FFmpeg stack for amd64 and arm64 ---
COPY lib/ffmpeg lib/ffmpeg
RUN cd lib/ffmpeg \
    && ./configure --prefix=/opt/ffmpeg/amd64 \
        --disable-programs --disable-doc --disable-debug --disable-autodetect \
        --disable-network --disable-openssl --disable-iconv --disable-zlib --disable-bzlib --disable-lzma \
        --disable-avdevice --disable-avfilter --disable-swscale \
        --disable-everything --disable-gpl --disable-nonfree \
        --enable-static --disable-shared \
        --enable-avcodec --enable-avformat --enable-avutil --enable-swresample \
        --enable-decoder=aac --enable-decoder=aac_latm \
        --enable-decoder=ac3 --enable-decoder=eac3 --enable-decoder=alac \
        --enable-decoder=ape --enable-decoder=amrnb --enable-decoder=amrwb \
        --enable-decoder=flac --enable-decoder=g723_1 --enable-decoder=gsm \
        --enable-decoder=mp1 --enable-decoder=mp2 --enable-decoder=mp3 \
        --enable-decoder=mpc7 --enable-decoder=mpc8 --enable-decoder=opus \
        --enable-decoder=speex --enable-decoder=tta --enable-decoder=wavpack \
        --enable-decoder=vorbis --enable-decoder=wmalossless \
        --enable-decoder=wmapro --enable-decoder=wmav1 --enable-decoder=wmav2 \
        --enable-decoder=pcm_s8 --enable-decoder=pcm_u8 \
        --enable-decoder=pcm_s16le --enable-decoder=pcm_s16be \
        --enable-decoder=pcm_s24le --enable-decoder=pcm_s24be \
        --enable-decoder=pcm_s32le --enable-decoder=pcm_s32be \
        --enable-decoder=pcm_f32le --enable-decoder=pcm_f32be \
        --enable-decoder=pcm_f64le --enable-decoder=pcm_f64be \
        --enable-parser=aac --enable-parser=ac3 --enable-parser=flac \
        --enable-parser=mpegaudio --enable-parser=opus --enable-parser=vorbis \
        --enable-demuxer=aac --enable-demuxer=ac3 --enable-demuxer=amr \
        --enable-demuxer=ape --enable-demuxer=asf --enable-demuxer=aiff \
        --enable-demuxer=caf --enable-demuxer=eac3 --enable-demuxer=flac \
        --enable-demuxer=matroska --enable-demuxer=mov --enable-demuxer=mp3 \
        --enable-demuxer=mpegts --enable-demuxer=ogg --enable-demuxer=rm \
        --enable-demuxer=mpc --enable-demuxer=mpc8 --enable-demuxer=tta \
        --enable-demuxer=wv --enable-demuxer=wav \
        --disable-x86asm \
    && make -j$(nproc) \
    && make install \
    && make distclean \
    && ./configure --prefix=/opt/ffmpeg/arm64 \
        --arch=aarch64 --target-os=linux --cross-prefix=aarch64-linux-gnu- \
        --cc=aarch64-linux-gnu-gcc --cxx=aarch64-linux-gnu-g++ \
        --ar=aarch64-linux-gnu-ar --ranlib=aarch64-linux-gnu-ranlib \
        --disable-programs --disable-doc --disable-debug --disable-autodetect \
        --disable-network --disable-openssl --disable-iconv --disable-zlib --disable-bzlib --disable-lzma \
        --disable-avdevice --disable-avfilter --disable-swscale \
        --disable-everything --disable-gpl --disable-nonfree \
        --enable-static --disable-shared \
        --enable-avcodec --enable-avformat --enable-avutil --enable-swresample \
        --enable-decoder=aac --enable-decoder=aac_latm \
        --enable-decoder=ac3 --enable-decoder=eac3 --enable-decoder=alac \
        --enable-decoder=ape --enable-decoder=amrnb --enable-decoder=amrwb \
        --enable-decoder=flac --enable-decoder=g723_1 --enable-decoder=gsm \
        --enable-decoder=mp1 --enable-decoder=mp2 --enable-decoder=mp3 \
        --enable-decoder=mpc7 --enable-decoder=mpc8 --enable-decoder=opus \
        --enable-decoder=speex --enable-decoder=tta --enable-decoder=wavpack \
        --enable-decoder=vorbis --enable-decoder=wmalossless \
        --enable-decoder=wmapro --enable-decoder=wmav1 --enable-decoder=wmav2 \
        --enable-decoder=pcm_s8 --enable-decoder=pcm_u8 \
        --enable-decoder=pcm_s16le --enable-decoder=pcm_s16be \
        --enable-decoder=pcm_s24le --enable-decoder=pcm_s24be \
        --enable-decoder=pcm_s32le --enable-decoder=pcm_s32be \
        --enable-decoder=pcm_f32le --enable-decoder=pcm_f32be \
        --enable-decoder=pcm_f64le --enable-decoder=pcm_f64be \
        --enable-parser=aac --enable-parser=ac3 --enable-parser=flac \
        --enable-parser=mpegaudio --enable-parser=opus --enable-parser=vorbis \
        --enable-demuxer=aac --enable-demuxer=ac3 --enable-demuxer=amr \
        --enable-demuxer=ape --enable-demuxer=asf --enable-demuxer=aiff \
        --enable-demuxer=caf --enable-demuxer=eac3 --enable-demuxer=flac \
        --enable-demuxer=matroska --enable-demuxer=mov --enable-demuxer=mp3 \
        --enable-demuxer=mpegts --enable-demuxer=ogg --enable-demuxer=rm \
        --enable-demuxer=mpc --enable-demuxer=mpc8 --enable-demuxer=tta \
        --enable-demuxer=wv --enable-demuxer=wav \
        --disable-x86asm \
    && make -j$(nproc) \
    && make install

# --- Pre-build font subset ---
COPY internal/ui/font_ranges.json /tmp/font_ranges.json
RUN mkdir -p /opt/font \
    && wget -q https://unifoundry.com/pub/unifont/unifont-${UNIFONT_VERSION}/font-builds/unifont-${UNIFONT_VERSION}.otf -O /tmp/unifont-full.otf \
    && python3 -m fontTools.subset /tmp/unifont-full.otf \
        --unicodes=$(python3 -c "import json; print(','.join(json.load(open('/tmp/font_ranges.json'))))") \
        --no-subset-tables+=OS/2 \
        --no-prune-unicode-ranges \
        --output-file=/opt/font/unifont.otf 2>&1 \
    && rm -f /tmp/unifont-full.otf /tmp/font_ranges.json

# Pre-download Go dependencies
COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Install a reproducible golangci-lint version using the container Go toolchain
RUN GOBIN=/usr/local/bin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2

# Copy SoLoud headers
COPY lib/soloud lib/soloud
RUN mkdir -p /opt/soloud/include && cp -r lib/soloud/include/* /opt/soloud/include/

# Export headers and place generated export header into source include tree
RUN cp lib/projectm/build-amd64/src/api/include/projectM-4/projectM_export.h lib/projectm/src/api/include/projectM-4/ \
    && cp lib/projectm/build-amd64/src/api/include/projectM-4/version.h lib/projectm/src/api/include/projectM-4/ \
    && mkdir -p /opt/projectm/include \
    && cp -r lib/projectm/src/api/include/* /opt/projectm/include/
