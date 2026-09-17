APP      := glitchscope
VERSION  ?= 1.0
DIST_DIR := dist
LOCAL_DIST_DIR := $(DIST_DIR)/local
X64_DIST_DIR := $(DIST_DIR)/linux-amd64
ARM64_DIST_DIR := $(DIST_DIR)/linux-arm64
WINDOWS_DIST_DIR := $(DIST_DIR)/windows-amd64
FONT_SUBSET := internal/ui/assets/unifont.otf
FONT_RANGES := internal/ui/font_ranges.json
FONT_REQUIRED := U+2014,U+2026,U+2192,U+23F8,U+25B6,U+25B8,U+25C0
FONT_URL := https://unifoundry.com/pub/unifont/unifont-17.0.05/font-builds/unifont-17.0.05.otf
ICON_MANIFEST := internal/ui/icon_assets.json
ICON_ASSETS := internal/ui/assets/icons
GO       := go
GOFLAGS  := CGO_ENABLED=1

PRESETS_REPO       := https://github.com/projectM-visualizer/presets-cream-of-the-crop.git
PRESETS_DIR        := dist/presets-cream-of-the-crop
GSA_FILE           := dist/presets.gsa
FULL_GSA_FILE      := dist/presets-all.gsa
BENCHMARK_CSV      := docs/benchmark/render-scale-0.5_mesh-8.csv
TEXTURES_REPO      := https://github.com/projectM-visualizer/presets-milkdrop-texture-pack.git
TEXTURES_DIR       := dist/presets-milkdrop-texture-pack
OPTIMIZED_TEXTURES := dist/textures-optimized
TEXTURES_GSA_FILE  := dist/textures.gsa
TEXTURE_REPORT     := docs/texture-usage-report.csv
PORTS_DIR          := /userdata/roms/ports
DEVICE_DIR         := $(PORTS_DIR)/glitchscope
PM_AUTOINSTALL     := /userdata/system/.local/share/PortMaster/autoinstall

SDL_CFLAGS := $(shell pkg-config --cflags sdl2 2>/dev/null)
SDL_LIBS   := $(shell pkg-config --libs sdl2 2>/dev/null)

PROJECTM_DIR      := lib/projectm
PROJECTM_BUILD    := $(PROJECTM_DIR)/build
PROJECTM_LIB      := $(PROJECTM_BUILD)/src/libprojectM/libprojectM-4.a
PROJECTM_EVAL_LIB := $(PROJECTM_BUILD)/vendor/projectm-eval/projectm-eval/libprojectM_eval.a

CGO_CXXFLAGS := $(SDL_CFLAGS) -I/opt/xmp/amd64/include -I/opt/openmpt/amd64/include -I/opt/gme/amd64/include -I/opt/ayumi/include -I/opt/pt3player/include -I/opt/libstsound/include -I/opt/crsid/include -I/opt/ffmpeg/amd64/include -I/build/lib/soloud/include -I/build/lib/game-music-emu/gme -Wno-write-strings
CGO_LDFLAGS  := $(SDL_LIBS) /opt/pt3player/amd64/lib/libpt3player.a /opt/ayumi/amd64/lib/libayumi.a /opt/libstsound/amd64/lib/libstsound.a /opt/crsid/amd64/lib/libcrsid.a /opt/ffmpeg/amd64/lib/libavformat.a /opt/ffmpeg/amd64/lib/libavcodec.a /opt/ffmpeg/amd64/lib/libswresample.a /opt/ffmpeg/amd64/lib/libavutil.a $(PROJECTM_LIB) $(PROJECTM_EVAL_LIB) -ldl -lGL -lGLESv2 -lm -pthread /opt/xmp/amd64/lib/libxmp.a /opt/openmpt/amd64/lib/libopenmpt.a /opt/gme/amd64/lib/libgme.a -lvorbisfile -lvorbis -lFLAC -logg -lmpg123 -lz -lstdc++

# Shared amd64 cgo environment for Docker-based development checks.
DOCKER_GO_ENV := GOFLAGS=-buildvcs=false CGO_ENABLED=1 \
	CGO_CFLAGS="-I/opt/xmp/amd64/include -I/opt/openmpt/amd64/include -I/opt/crsid/include -I/opt/ffmpeg/amd64/include -I/usr/include/SDL2 -D_REENTRANT" \
	CGO_CXXFLAGS="-std=c++11 -Wno-write-strings -DWITH_SDL2_STATIC -I/opt/soloud/include -I/opt/ayumi/include -I/opt/pt3player/include -I/opt/libstsound/include -I/opt/crsid/include -I/opt/ffmpeg/amd64/include -I/opt/projectm/include -I/opt/projectm/include/projectM-4 -I/opt/gme/amd64/include -I/opt/xmp/amd64/include -I/opt/openmpt/amd64/include -I/usr/include/SDL2 -D_REENTRANT" \
	CGO_LDFLAGS="-lSDL2 /opt/pt3player/amd64/lib/libpt3player.a /opt/ayumi/amd64/lib/libayumi.a /opt/libstsound/amd64/lib/libstsound.a /opt/crsid/amd64/lib/libcrsid.a /opt/ffmpeg/amd64/lib/libavformat.a /opt/ffmpeg/amd64/lib/libavcodec.a /opt/ffmpeg/amd64/lib/libswresample.a /opt/ffmpeg/amd64/lib/libavutil.a /opt/projectm/amd64/lib/libprojectM-4.a /opt/projectm/amd64/lib/libprojectM_eval.a -lGL -lGLESv2 -lm -pthread /opt/xmp/amd64/lib/libxmp.a /opt/openmpt/amd64/lib/libopenmpt.a /opt/gme/amd64/lib/libgme.a -lvorbisfile -lvorbis -lFLAC -logg -lmpg123 -lz -lstdc++"

DOCKER_IMAGE_X64 := glitchscope-builder
DOCKER_BUILDER   := glitchscope-builder:latest
DOCKER_GO_CACHE  := glitchscope-go-build-cache

ALLMODS_URL := https://modland.antarctica.no/allmods.zip
ALLMODS_ZIP := $(DIST_DIR)/allmods.zip
MODLAND_CATALOG    := .cache/modland/catalog
MODARCHIVE_CATALOG := .cache/modarchive/catalog
MODARCHIVE_SNAPSHOT := .cache/modarchive/1980-2007.gsa
MODARCHIVE_ADDENDUM := .cache/modarchive/2007-addendum.gsa

.PHONY: builder build clean dist dist-arm64 dist-windows dist-portmaster lint run run-local projectm-build submodules test tidy presets glitchscope portable-glitchscope textures optimize-textures texture-archive texture-report catalog catalog-validate modland-catalog modarchive-catalog deploy deploy-music deploy-fast deploy-portmaster kill subset-font icons

DOCKER_DEV_RUN = docker run --rm -v "$(CURDIR):/build" -v "$(DOCKER_GO_CACHE):/root/.cache/go-build" -w /build $(DOCKER_BUILDER) bash -c
DOCKER_ICON_RUN = docker run --rm --user "$$(id -u):$$(id -g)" -v "$(CURDIR):/build" -w /build $(DOCKER_BUILDER) bash -c

# Builder image: C/C++ static dependencies compiled once for amd64 & arm64
builder:
	docker build -t $(DOCKER_BUILDER) -f Dockerfile.builder .

subset-font:
	@./scripts/subset-font.sh

icons: builder
	$(DOCKER_ICON_RUN) 'python3 scripts/generate-icons.py --manifest $(ICON_MANIFEST) --output $(ICON_ASSETS)'

lint: icons builder
	$(DOCKER_DEV_RUN) '$(DOCKER_GO_ENV) golangci-lint run --verbose --timeout=5m ./cmd/... ./internal/...'

test: icons builder
	$(DOCKER_DEV_RUN) '$(DOCKER_GO_ENV) go test -count=1 ./cmd/... ./internal/...'

# Build modarchive catalog via crawler (only if missing).
$(MODARCHIVE_CATALOG): builder
	@if [ ! -f "$@" ]; then \
		$(DOCKER_DEV_RUN) '$(DOCKER_GO_ENV) go run ./cmd/modarchive-catalog -catalog-only -v'; \
		echo "=== Built modarchive catalog $@ ==="; \
	else \
		echo "=== Modarchive catalog $@ already exists, skipping ==="; \
	fi

$(MODARCHIVE_SNAPSHOT): builder
	@if [ ! -f "$@" ]; then \
		$(DOCKER_DEV_RUN) '$(DOCKER_GO_ENV) go run ./cmd/modarchive-catalog -snapshot-only -v'; \
		echo "=== Built modarchive snapshot $@ ==="; \
	else \
		echo "=== Modarchive snapshot $@ already exists, skipping ==="; \
	fi

$(MODARCHIVE_ADDENDUM): builder
	@if [ ! -f "$@" ]; then \
		$(DOCKER_DEV_RUN) '$(DOCKER_GO_ENV) go run ./cmd/modarchive-catalog -addendum-only -v'; \
		echo "=== Built modarchive addendum $@ ==="; \
	else \
		echo "=== Modarchive addendum $@ already exists, skipping ==="; \
	fi

modland-catalog: $(MODLAND_CATALOG)
modarchive-catalog: $(MODARCHIVE_CATALOG) $(MODARCHIVE_SNAPSHOT) $(MODARCHIVE_ADDENDUM)

# Docker build (amd64)
dist: subset-font icons builder $(GSA_FILE) $(TEXTURES_GSA_FILE) $(MODLAND_CATALOG) $(MODARCHIVE_CATALOG) $(MODARCHIVE_SNAPSHOT) $(MODARCHIVE_ADDENDUM)
	docker build --build-arg BUILDER_IMAGE=$(DOCKER_BUILDER) --build-arg TARGETARCH=amd64 --build-arg APP_VERSION=$(VERSION) -t glitchscope:amd64 -f Dockerfile .
	@# Backup music directory if it exists before rm -rf.
	@if [ -d "$(X64_DIST_DIR)/music" ]; then \
		cp -r "$(X64_DIST_DIR)/music" "$(X64_DIST_DIR)/music.bak"; \
	fi
	@rm -rf $(X64_DIST_DIR)
	@mkdir -p $(X64_DIST_DIR)
	@docker rm -f glitchscope-extract-x64 2>/dev/null || true
	docker create --name glitchscope-extract-x64 glitchscope:amd64
	docker cp glitchscope-extract-x64:/dist/glitchscope/. $(X64_DIST_DIR)/
	docker rm glitchscope-extract-x64
	@# Replace test presets with the real .gsa archive.
	rm -rf $(X64_DIST_DIR)/presets/*
	cp $(GSA_FILE) $(X64_DIST_DIR)/presets/presets.gsa
	rm -rf $(X64_DIST_DIR)/textures
	cp $(TEXTURES_GSA_FILE) $(X64_DIST_DIR)/presets/textures.gsa
	@mkdir -p $(X64_DIST_DIR)/.cache/modland $(X64_DIST_DIR)/.cache/modarchive
	@cp $(MODLAND_CATALOG) $(X64_DIST_DIR)/.cache/modland/catalog
	@cp $(MODARCHIVE_CATALOG) $(X64_DIST_DIR)/.cache/modarchive/catalog
	@cp $(MODARCHIVE_SNAPSHOT) $(X64_DIST_DIR)/.cache/modarchive/1980-2007.gsa
	@cp $(MODARCHIVE_ADDENDUM) $(X64_DIST_DIR)/.cache/modarchive/2007-addendum.gsa
	@# Restore music directory from backup.
	@if [ -d "$(X64_DIST_DIR)/music.bak" ]; then \
		mv "$(X64_DIST_DIR)/music.bak" "$(X64_DIST_DIR)/music"; \
	fi
	@echo "=== Built $(APP) (amd64) ==="
	@ls -lhR $(X64_DIST_DIR)/

# ARM64 cross-build via Docker
DOCKER_IMAGE_ARM64 := glitchscope:arm64
DOCKER_IMAGE_WINDOWS := glitchscope-windows

dist-arm64: subset-font icons builder portable-glitchscope $(TEXTURES_GSA_FILE) $(MODLAND_CATALOG) $(MODARCHIVE_CATALOG) $(MODARCHIVE_SNAPSHOT) $(MODARCHIVE_ADDENDUM)
	docker build --build-arg BUILDER_IMAGE=$(DOCKER_BUILDER) --build-arg TARGETARCH=arm64 --build-arg APP_VERSION=$(VERSION) -t $(DOCKER_IMAGE_ARM64) -f Dockerfile .
	@rm -rf $(ARM64_DIST_DIR)
	@mkdir -p $(ARM64_DIST_DIR)
	@docker rm -f glitchscope-extract 2>/dev/null || true
	docker create --name glitchscope-extract $(DOCKER_IMAGE_ARM64)
	docker cp glitchscope-extract:/dist/. $(ARM64_DIST_DIR)/
	docker rm glitchscope-extract
	rm -rf $(ARM64_DIST_DIR)/glitchscope/presets/*
	cp $(GSA_FILE) $(ARM64_DIST_DIR)/glitchscope/presets/presets.gsa
	rm -rf $(ARM64_DIST_DIR)/glitchscope/textures
	cp $(TEXTURES_GSA_FILE) $(ARM64_DIST_DIR)/glitchscope/presets/textures.gsa
	@mkdir -p $(ARM64_DIST_DIR)/glitchscope/.cache/modland $(ARM64_DIST_DIR)/glitchscope/.cache/modarchive
	@cp $(MODLAND_CATALOG) $(ARM64_DIST_DIR)/glitchscope/.cache/modland/catalog
	@cp $(MODARCHIVE_CATALOG) $(ARM64_DIST_DIR)/glitchscope/.cache/modarchive/catalog
	@cp $(MODARCHIVE_SNAPSHOT) $(ARM64_DIST_DIR)/glitchscope/.cache/modarchive/1980-2007.gsa
	@cp $(MODARCHIVE_ADDENDUM) $(ARM64_DIST_DIR)/glitchscope/.cache/modarchive/2007-addendum.gsa
	@# Deploy prebuilt shuffle indexes (platform-independent GSA archives).
	@mkdir -p $(ARM64_DIST_DIR)/glitchscope/.cache/shuffle
	@cp $(ARM64_DIST_DIR)/../linux-amd64/.cache/shuffle/*.idx $(ARM64_DIST_DIR)/glitchscope/.cache/shuffle/ 2>/dev/null || true
	@echo "=== $(ARM64_DIST_DIR)/ ==="
	@ls -lhR $(ARM64_DIST_DIR)/

# Windows amd64 cross-build. The dedicated image owns the Windows CRT,
# OpenGL/GLEW, SDL2 and static native dependency toolchain.
dist-windows: subset-font icons $(GSA_FILE) $(TEXTURES_GSA_FILE) $(MODLAND_CATALOG) $(MODARCHIVE_CATALOG) $(MODARCHIVE_SNAPSHOT) $(MODARCHIVE_ADDENDUM)
	docker build --build-arg APP_VERSION=$(VERSION) -t $(DOCKER_IMAGE_WINDOWS) -f Dockerfile.windows .
	@rm -rf $(WINDOWS_DIST_DIR)
	@mkdir -p $(WINDOWS_DIST_DIR)
	@docker rm -f glitchscope-extract-windows 2>/dev/null || true
	docker create --name glitchscope-extract-windows $(DOCKER_IMAGE_WINDOWS) /bin/false
	docker cp glitchscope-extract-windows:/dist/glitchscope/. $(WINDOWS_DIST_DIR)/
	docker rm glitchscope-extract-windows
	@# Replace the empty runtime asset directories with the release archives.
	rm -rf $(WINDOWS_DIST_DIR)/presets/*
	cp $(GSA_FILE) $(WINDOWS_DIST_DIR)/presets/presets.gsa
	rm -rf $(WINDOWS_DIST_DIR)/textures
	cp $(TEXTURES_GSA_FILE) $(WINDOWS_DIST_DIR)/presets/textures.gsa
	@# Include the offline catalogs shipped by the Linux release.
	@mkdir -p $(WINDOWS_DIST_DIR)/.cache/modland $(WINDOWS_DIST_DIR)/.cache/modarchive
	@cp $(MODLAND_CATALOG) $(WINDOWS_DIST_DIR)/.cache/modland/catalog
	@cp $(MODARCHIVE_CATALOG) $(WINDOWS_DIST_DIR)/.cache/modarchive/catalog
	@cp $(MODARCHIVE_SNAPSHOT) $(WINDOWS_DIST_DIR)/.cache/modarchive/1980-2007.gsa
	@cp $(MODARCHIVE_ADDENDUM) $(WINDOWS_DIST_DIR)/.cache/modarchive/2007-addendum.gsa
	@echo "=== Built $(APP) (Windows amd64) ==="
	@ls -lhR $(WINDOWS_DIST_DIR)/

# PortMaster packaging — structure must match zimlite (gameinfo.xml, README.md at root).
dist-portmaster: dist-arm64 portable-glitchscope $(TEXTURES_GSA_FILE) $(MODLAND_CATALOG) $(MODARCHIVE_CATALOG) $(MODARCHIVE_SNAPSHOT) $(MODARCHIVE_ADDENDUM)
	@rm -rf dist/portmaster_build
	@mkdir -p dist/portmaster_build/glitchscope
	@# Laucher script in zip root
	cp portmaster/GlitchScope.sh dist/portmaster_build/
	@# Everything else goes INSIDE the glitchscope/ folder
	cp $(ARM64_DIST_DIR)/glitchscope/glitchscope dist/portmaster_build/glitchscope/
	cp $(GSA_FILE) dist/portmaster_build/glitchscope/presets/presets.gsa
	cp $(TEXTURES_GSA_FILE) dist/portmaster_build/glitchscope/presets/textures.gsa
	@mkdir -p dist/portmaster_build/glitchscope/.cache/modland dist/portmaster_build/glitchscope/.cache/modarchive
	@cp $(MODLAND_CATALOG) dist/portmaster_build/glitchscope/.cache/modland/catalog
	@cp $(MODARCHIVE_CATALOG) dist/portmaster_build/glitchscope/.cache/modarchive/catalog
	@cp $(MODARCHIVE_SNAPSHOT) dist/portmaster_build/glitchscope/.cache/modarchive/1980-2007.gsa
	@cp $(MODARCHIVE_ADDENDUM) dist/portmaster_build/glitchscope/.cache/modarchive/2007-addendum.gsa
	@# Copy prebuilt shuffle indexes (platform-independent).
	@mkdir -p dist/portmaster_build/glitchscope/.cache/shuffle
	@cp $(ARM64_DIST_DIR)/glitchscope/.cache/shuffle/*.idx dist/portmaster_build/glitchscope/.cache/shuffle/ 2>/dev/null || true
	cp portmaster/port.json dist/portmaster_build/glitchscope/
	cp portmaster/screenshot.png dist/portmaster_build/glitchscope/
	cp portmaster/gameinfo.xml dist/portmaster_build/glitchscope/ 2>/dev/null; true
	cp portmaster/licenses/* dist/portmaster_build/glitchscope/licenses/ 2>/dev/null; true
	@# Download demo tracker music
	@mkdir -p dist/portmaster_build/glitchscope/music
	@echo "=== Downloading demo tracks ==="
	@curl -sL -o dist/portmaster_build/glitchscope/music/aryx.s3m "https://api.modarchive.org/downloads.php?moduleid=191789"
	@curl -sL -o dist/portmaster_build/glitchscope/music/external.xm "https://api.modarchive.org/downloads.php?moduleid=66187"
	@curl -sL -o dist/portmaster_build/glitchscope/music/ELYSIUM.MOD "https://api.modarchive.org/downloads.php?moduleid=40475"
	@curl -sL -o dist/portmaster_build/glitchscope/music/sick-ass.it "https://api.modarchive.org/downloads.php?moduleid=177712"
	@echo "=== Demo tracks downloaded ==="
	@ls -lh dist/portmaster_build/glitchscope/music/
	@# Zip: root = .sh + glitchscope/ folder only
	@rm -f dist/glitchscope.zip
	cd dist/portmaster_build && zip -r ../glitchscope.zip "GlitchScope.sh" glitchscope
	@echo "=== Generated dist/glitchscope.zip ==="
	@ls -lh dist/glitchscope.zip

deploy: dist-arm64 portable-glitchscope $(TEXTURES_GSA_FILE)
	adb shell "mkdir -p $(DEVICE_DIR)"
	adb push $(ARM64_DIST_DIR)/glitchscope/glitchscope $(DEVICE_DIR)/
	adb push portmaster/GlitchScope.sh $(PORTS_DIR)/
	# Deploy presets as single .gsa archive (fast on FAT32).
	adb shell "mkdir -p $(DEVICE_DIR)/presets"
	adb push $(GSA_FILE) $(DEVICE_DIR)/presets/presets.gsa
	# Deploy the optimized texture archive for MilkDrop presets.
	adb push $(TEXTURES_GSA_FILE) $(DEVICE_DIR)/presets/textures.gsa
	# Deploy the prebuilt Modland and ModArchive catalogs.
	adb shell "mkdir -p $(DEVICE_DIR)/.cache/modland $(DEVICE_DIR)/.cache/modarchive"
	adb push $(ARM64_DIST_DIR)/glitchscope/.cache/modland/catalog $(DEVICE_DIR)/.cache/modland/catalog
	adb push $(ARM64_DIST_DIR)/glitchscope/.cache/modarchive/catalog $(DEVICE_DIR)/.cache/modarchive/catalog
	adb push $(ARM64_DIST_DIR)/glitchscope/.cache/modarchive/1980-2007.gsa $(DEVICE_DIR)/.cache/modarchive/1980-2007.gsa
	adb push $(ARM64_DIST_DIR)/glitchscope/.cache/modarchive/2007-addendum.gsa $(DEVICE_DIR)/.cache/modarchive/2007-addendum.gsa
	# Deploy prebuilt shuffle indexes.
	adb shell "mkdir -p $(DEVICE_DIR)/.cache/shuffle"
	adb push $(ARM64_DIST_DIR)/glitchscope/.cache/shuffle/ $(DEVICE_DIR)/.cache/shuffle/
	adb shell "killall -9 glitchscope 2>/dev/null; true"
	@echo "=== Deployed binary + presets + textures + catalogs + shuffle indexes ==="

# Push test music to device (optional, for quick testing).
deploy-music:
	adb shell "mkdir -p $(DEVICE_DIR)/music"
	adb push test_data/* $(DEVICE_DIR)/music
	@echo "=== Deployed test music to $(DEVICE_DIR)/music ==="

deploy-fast: dist-arm64
	adb push $(ARM64_DIST_DIR)/glitchscope/glitchscope $(DEVICE_DIR)/
	adb shell "killall -9 glitchscope 2>/dev/null; true"
	@echo "=== Deployed binary only ==="

deploy-portmaster: dist-portmaster
	adb push dist/glitchscope.zip $(PM_AUTOINSTALL)/
	@echo "=== Deployed to autoinstall ==="

kill:
	adb shell "killall -9 glitchscope 2>/dev/null || true"
	@echo "=== Killed glitchscope on device ==="

# Download cream-of-the-crop presets (9.8K presets, ~160MB).
presets:
	@if [ -d "$(PRESETS_DIR)" ]; then \
		echo "Presets already in $(PRESETS_DIR). Delete and rerun to re-clone."; \
	else \
		git clone --depth 1 $(PRESETS_REPO) $(PRESETS_DIR); \
		echo "=== Downloaded cream-of-the-crop presets ==="; \
	fi

# Download milkdrop texture pack (~15MB, needed for presets with user textures).
textures:
	@if [ -d "$(TEXTURES_DIR)" ]; then \
		echo "Textures already in $(TEXTURES_DIR). Delete and rerun to re-clone."; \
	else \
		git clone --depth 1 $(TEXTURES_REPO) $(TEXTURES_DIR); \
		echo "=== Downloaded milkdrop texture pack ==="; \
	fi

optimize-textures: textures
	./scripts/optimize-textures.sh $(TEXTURES_DIR)/textures $(OPTIMIZED_TEXTURES) docs/texture-optimization.csv

texture-archive:
	@rm -f $(TEXTURES_GSA_FILE)
	$(MAKE) $(TEXTURES_GSA_FILE)

$(TEXTURES_GSA_FILE):
	@if [ ! -f "$@" ]; then \
		mkdir -p $(dir $@); \
		if [ ! -d "$(OPTIMIZED_TEXTURES)" ]; then \
			$(MAKE) optimize-textures; \
		fi; \
		$(GO) run ./cmd/glitchscope-pack textures $(OPTIMIZED_TEXTURES) $@; \
		echo "=== Built texture archive $@ ==="; \
	else \
		echo "=== Texture archive $@ already exists, skipping ==="; \
	fi

texture-report: presets cmd/texture-report/main.go
	go run ./cmd/texture-report $(PRESETS_DIR) $(TEXTURE_REPORT)

# Build presets.gsa archive from cream-of-the-crop dir (only if missing).
$(FULL_GSA_FILE):
	@if [ ! -f "$@" ]; then \
		$(MAKE) presets; \
		mkdir -p $(dir $@); \
		$(GO) run ./cmd/glitchscope-pack presets $(PRESETS_DIR) $@; \
		echo "=== Built full preset archive $@ ==="; \
	else \
		echo "=== Full preset archive $@ already exists, skipping ==="; \
	fi

glitchscope: $(FULL_GSA_FILE)

$(GSA_FILE):
	@if [ ! -f "$@" ]; then \
		$(MAKE) presets; \
		mkdir -p $(dir $@); \
		$(GO) run ./cmd/glitchscope-pack presets $(PRESETS_DIR) $@ $(BENCHMARK_CSV); \
		echo "=== Built filtered portable preset archive $@ ==="; \
	else \
		echo "=== Preset archive $@ already exists, skipping ==="; \
	fi

portable-glitchscope: $(GSA_FILE)

# Download allmods.zip listing from modland.com (only if missing).
$(ALLMODS_ZIP):
	@mkdir -p $(dir $@)
	curl -fL -o $@ $(ALLMODS_URL)
	@echo "=== Downloaded $(ALLMODS_URL) → $@ ==="

# Build modland catalog from allmods.zip (only if missing).
$(MODLAND_CATALOG): builder
	@if [ ! -f "$@" ]; then \
		$(MAKE) $(ALLMODS_ZIP); \
		mkdir -p $(dir $@); \
		$(DOCKER_DEV_RUN) '$(DOCKER_GO_ENV) go run ./cmd/modland-catalog $(ALLMODS_ZIP) .'; \
		echo "=== Built modland catalog $@ ==="; \
	else \
		echo "=== Modland catalog $@ already exists, skipping ==="; \
	fi

# Validate catalog: build validator in Docker, run with network to test each format.
catalog-validate: $(MODLAND_CATALOG) builder
	$(DOCKER_DEV_RUN) '$(DOCKER_GO_ENV) go run ./cmd/validate-catalog /build/'

catalog: modland-catalog modarchive-catalog catalog-validate
