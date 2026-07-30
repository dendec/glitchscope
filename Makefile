APP      := pmv
DIST_DIR := dist
LOCAL_DIST_DIR := $(DIST_DIR)/local
X64_DIST_DIR := $(DIST_DIR)/linux-amd64
ARM64_DIST_DIR := $(DIST_DIR)/linux-arm64
FONT_SUBSET := internal/ui/assets/unifont.otf
FONT_RANGES := internal/ui/font_ranges.json
FONT_REQUIRED := U+2014,U+2026,U+2192,U+23F8,U+25B6,U+25B8,U+25C0
FONT_URL := https://unifoundry.com/pub/unifont/unifont-17.0.05/font-builds/unifont-17.0.05.otf
GO       := go
GOFLAGS  := CGO_ENABLED=1

PRESETS_REPO       := https://github.com/projectM-visualizer/presets-cream-of-the-crop.git
PRESETS_DIR        := dist/presets-cream-of-the-crop
PMV_FILE           := dist/presets.pmv
FULL_PMV_FILE      := dist/presets-all.pmv
BENCHMARK_CSV      := docs/benchmark/render-scale-0.5_mesh-8.csv
TEXTURES_REPO      := https://github.com/projectM-visualizer/presets-milkdrop-texture-pack.git
TEXTURES_DIR       := dist/presets-milkdrop-texture-pack
OPTIMIZED_TEXTURES := dist/textures-optimized
TEXTURES_PMV_FILE  := dist/textures.pmv
TEXTURE_REPORT     := docs/texture-usage-report.csv
PORTS_DIR          := /userdata/roms/ports
DEVICE_DIR         := $(PORTS_DIR)/pmv
PM_AUTOINSTALL     := /userdata/system/.local/share/PortMaster/autoinstall

SDL_CFLAGS := $(shell pkg-config --cflags sdl2 2>/dev/null)
SDL_LIBS   := $(shell pkg-config --libs sdl2 2>/dev/null)

PROJECTM_DIR      := lib/projectm
PROJECTM_BUILD    := $(PROJECTM_DIR)/build
PROJECTM_LIB      := $(PROJECTM_BUILD)/src/libprojectM/libprojectM-4.a
PROJECTM_EVAL_LIB := $(PROJECTM_BUILD)/vendor/projectm-eval/projectm-eval/libprojectM_eval.a
PROJECTM_PATCH    := patches/projectm-feedback.patch

CGO_CXXFLAGS := $(SDL_CFLAGS) -I/opt/xmp/amd64/include -I/opt/openmpt/amd64/include -I/opt/gme/amd64/include -I/opt/ayumi/include -I/opt/pt3player/include -I/opt/libstsound/include -I/build/lib/soloud/include -I/build/lib/game-music-emu/gme -Wno-write-strings
CGO_LDFLAGS  := $(SDL_LIBS) /opt/pt3player/amd64/lib/libpt3player.a /opt/ayumi/amd64/lib/libayumi.a /opt/libstsound/amd64/lib/libstsound.a $(PROJECTM_LIB) $(PROJECTM_EVAL_LIB) -ldl -lGL -lGLESv2 -lm /opt/xmp/amd64/lib/libxmp.a /opt/openmpt/amd64/lib/libopenmpt.a /opt/gme/amd64/lib/libgme.a -lvorbisfile -lvorbis -lFLAC -logg -lmpg123 -lz -lstdc++

DOCKER_IMAGE_X64 := pmv-builder
DOCKER_BUILDER   := pmv-builder:latest

ALLMODS_URL := https://modland.antarctica.no/allmods.zip
ALLMODS_ZIP := $(DIST_DIR)/allmods.zip
CATALOG     := dist/modland

.PHONY: builder build clean dist dist-arm64 dist-portmaster lint run run-local projectm-build submodules test tidy presets pmv portable-pmv textures optimize-textures texture-archive texture-report catalog catalog-validate

DOCKER_DEV_RUN = docker run --rm -v "$(CURDIR):/build" -w /build $(DOCKER_BUILDER) bash -c

# Builder image: C/C++ static dependencies compiled once for amd64 & arm64
builder:
	docker build -t $(DOCKER_BUILDER) -f Dockerfile.builder .

lint: builder
	$(DOCKER_DEV_RUN) 'GOFLAGS=-buildvcs=false CGO_ENABLED=1 CGO_CFLAGS="-I/opt/xmp/amd64/include -I/opt/openmpt/amd64/include -I/usr/include/SDL2 -D_REENTRANT" CGO_CXXFLAGS="-std=c++11 -Wno-write-strings -DWITH_SDL2_STATIC -I/opt/soloud/include -I/opt/ayumi/include -I/opt/pt3player/include -I/opt/libstsound/include -I/opt/projectm/include -I/opt/projectm/include/projectM-4 -I/opt/gme/amd64/include -I/opt/xmp/amd64/include -I/opt/openmpt/amd64/include -I/usr/include/SDL2 -D_REENTRANT" CGO_LDFLAGS="-lSDL2 /opt/pt3player/amd64/lib/libpt3player.a /opt/ayumi/amd64/lib/libayumi.a /opt/libstsound/amd64/lib/libstsound.a /opt/projectm/amd64/lib/libprojectM-4.a /opt/projectm/amd64/lib/libprojectM_eval.a -lGL -lGLESv2 -lm /opt/xmp/amd64/lib/libxmp.a /opt/openmpt/amd64/lib/libopenmpt.a /opt/gme/amd64/lib/libgme.a -lvorbisfile -lvorbis -lFLAC -logg -lmpg123 -lz -lstdc++" golangci-lint run --timeout=5m ./cmd/... ./internal/...'

test: builder
	$(DOCKER_DEV_RUN) 'GOFLAGS=-buildvcs=false CGO_ENABLED=1 CGO_CFLAGS="-I/opt/xmp/amd64/include -I/opt/openmpt/amd64/include -I/usr/include/SDL2 -D_REENTRANT" CGO_CXXFLAGS="-std=c++11 -Wno-write-strings -DWITH_SDL2_STATIC -I/opt/soloud/include -I/opt/ayumi/include -I/opt/pt3player/include -I/opt/libstsound/include -I/opt/projectm/include -I/opt/projectm/include/projectM-4 -I/opt/gme/amd64/include -I/opt/xmp/amd64/include -I/opt/openmpt/amd64/include -I/usr/include/SDL2 -D_REENTRANT" CGO_LDFLAGS="-lSDL2 /opt/pt3player/amd64/lib/libpt3player.a /opt/ayumi/amd64/lib/libayumi.a /opt/libstsound/amd64/lib/libstsound.a /opt/projectm/amd64/lib/libprojectM-4.a /opt/projectm/amd64/lib/libprojectM_eval.a -lGL -lGLESv2 -lm /opt/xmp/amd64/lib/libxmp.a /opt/openmpt/amd64/lib/libopenmpt.a /opt/gme/amd64/lib/libgme.a -lvorbisfile -lvorbis -lFLAC -logg -lmpg123 -lz -lstdc++" go test -count=1 ./cmd/... ./internal/...'

# Docker build (amd64)
dist: builder $(PMV_FILE) $(TEXTURES_PMV_FILE) $(CATALOG)
	docker build --build-arg BUILDER_IMAGE=$(DOCKER_BUILDER) --build-arg TARGETARCH=amd64 -t pmv:amd64 -f Dockerfile .
	@rm -rf $(X64_DIST_DIR)
	@mkdir -p $(X64_DIST_DIR)
	@docker rm -f pmv-extract-x64 2>/dev/null || true
	docker create --name pmv-extract-x64 pmv:amd64
	docker cp pmv-extract-x64:/dist/pmv/. $(X64_DIST_DIR)/
	docker rm pmv-extract-x64
	@# Replace test presets with the real .pmv archive.
	rm -rf $(X64_DIST_DIR)/presets/*
	cp $(PMV_FILE) $(X64_DIST_DIR)/presets/presets.pmv
	rm -rf $(X64_DIST_DIR)/textures
	cp $(TEXTURES_PMV_FILE) $(X64_DIST_DIR)/presets/textures.pmv
	cp -r test_data/* $(X64_DIST_DIR)/
	cp $(CATALOG) $(X64_DIST_DIR)/modland
	@echo "=== Built $(APP) (amd64) ==="
	@ls -lhR $(X64_DIST_DIR)/

# ARM64 cross-build via Docker
DOCKER_IMAGE_ARM64 := pmv:arm64

dist-arm64: builder portable-pmv $(TEXTURES_PMV_FILE) $(CATALOG)
	docker build --build-arg BUILDER_IMAGE=$(DOCKER_BUILDER) --build-arg TARGETARCH=arm64 -t $(DOCKER_IMAGE_ARM64) -f Dockerfile .
	@rm -rf $(ARM64_DIST_DIR)
	@mkdir -p $(ARM64_DIST_DIR)
	@docker rm -f pmv-extract 2>/dev/null || true
	docker create --name pmv-extract $(DOCKER_IMAGE_ARM64)
	docker cp pmv-extract:/dist/. $(ARM64_DIST_DIR)/
	docker rm pmv-extract
	rm -rf $(ARM64_DIST_DIR)/pmv/presets/*
	cp $(PMV_FILE) $(ARM64_DIST_DIR)/pmv/presets/presets.pmv
	rm -rf $(ARM64_DIST_DIR)/pmv/textures
	cp $(TEXTURES_PMV_FILE) $(ARM64_DIST_DIR)/pmv/presets/textures.pmv
	cp -r test_data/* $(ARM64_DIST_DIR)/
	cp $(CATALOG) $(ARM64_DIST_DIR)/pmv/modland
	@echo "=== $(ARM64_DIST_DIR)/ ==="
	@ls -lhR $(ARM64_DIST_DIR)/

# PortMaster packaging — structure must match zimlite (gameinfo.xml, README.md at root).
dist-portmaster: dist-arm64 portable-pmv $(TEXTURES_PMV_FILE) $(CATALOG)
	@rm -rf dist/portmaster_build
	@mkdir -p dist/portmaster_build/pmv/presets dist/portmaster_build/pmv/licenses
	cp portmaster/PMV.sh dist/portmaster_build/
	cp portmaster/port.json dist/portmaster_build/
	cp portmaster/README.md dist/portmaster_build/
	cp portmaster/screenshot.png dist/portmaster_build/
	cp portmaster/gameinfo.xml dist/portmaster_build/ 2>/dev/null; true
	@RELEASE_DATE=$$(date +%Y%m%d)T000000; \
	printf '<gameList>\n    <game>\n        <path>./PMV.sh</path>\n        <name>PMV</name>\n        <desc>Portable Music Visualizer — plays MP3/FLAC/Ogg/Mod/XM/IT/S3M with real-time MilkDrop visualizations. Drop your music into /roms/ports/pmv/music/ and enjoy a psychedelic audio experience on your handheld.</desc>\n        <image>./pmv/cover.png</image>\n        <developer>dendec</developer>\n        <publisher>dendec</publisher>\n        <releasedate>%s</releasedate>\n        <genre>Music</genre>\n    </game>\n</gameList>\n' "$$RELEASE_DATE" > dist/portmaster_build/pmv/gameinfo.xml
	cp $(ARM64_DIST_DIR)/pmv/pmv dist/portmaster_build/pmv/
	cp $(PMV_FILE) dist/portmaster_build/pmv/presets/presets.pmv
	cp $(TEXTURES_PMV_FILE) dist/portmaster_build/pmv/presets/textures.pmv
	cp $(CATALOG) dist/portmaster_build/pmv/modland
	cp portmaster/licenses/* dist/portmaster_build/pmv/licenses/ 2>/dev/null; true
	cp portmaster/screenshot.png dist/portmaster_build/pmv/cover.png 2>/dev/null; true
	@rm -f dist/pmv.zip
	cd dist/portmaster_build && zip -r ../pmv.zip "PMV.sh" README.md gameinfo.xml port.json screenshot.png pmv
	@echo "=== Generated dist/pmv.zip ==="
	@ls -lh dist/pmv.zip

deploy: dist-arm64 portable-pmv $(TEXTURES_PMV_FILE)
	adb shell "mkdir -p $(DEVICE_DIR)"
	adb push $(ARM64_DIST_DIR)/pmv/pmv $(DEVICE_DIR)/
	adb push portmaster/PMV.sh $(PORTS_DIR)/
	adb shell "mkdir -p $(DEVICE_DIR)/music"
	adb push test_data/* $(DEVICE_DIR)/music
	# Deploy presets as single .pmv archive (fast on FAT32).
	adb shell "mkdir -p $(DEVICE_DIR)/presets"
	adb push $(PMV_FILE) $(DEVICE_DIR)/presets/presets.pmv
	# Deploy the optimized texture archive for MilkDrop presets.
	adb push $(TEXTURES_PMV_FILE) $(DEVICE_DIR)/presets/textures.pmv
	# Deploy modland catalog.
	adb shell "mkdir -p $(DEVICE_DIR)/modland"
	adb push $(CATALOG) $(DEVICE_DIR)/modland/
	adb shell "killall -9 pmv 2>/dev/null; true"
	@echo "=== Deployed binary + music + presets + textures + catalog ==="

deploy-portmaster: dist-portmaster
	adb push dist/pmv.zip $(PM_AUTOINSTALL)/
	@echo "=== Deployed to autoinstall ==="
	adb shell "mkdir -p $(DEVICE_DIR)/music"
	adb push test_data/* $(DEVICE_DIR)/music
	@echo "=== Song deployed to $(DEVICE_DIR) ==="

kill:
	adb shell "killall -9 pmv 2>/dev/null || true"
	@echo "=== Killed pmv on device ==="

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
	@rm -f $(TEXTURES_PMV_FILE)
	$(MAKE) $(TEXTURES_PMV_FILE)

$(TEXTURES_PMV_FILE):
	@if [ ! -f "$@" ]; then \
		mkdir -p $(dir $@); \
		if [ ! -d "$(OPTIMIZED_TEXTURES)" ]; then \
			$(MAKE) optimize-textures; \
		fi; \
		$(GO) run ./cmd/pmv-pack textures $(OPTIMIZED_TEXTURES) $@; \
		echo "=== Built texture archive $@ ==="; \
	else \
		echo "=== Texture archive $@ already exists, skipping ==="; \
	fi

texture-report: presets cmd/texture-report/main.go
	go run ./cmd/texture-report $(PRESETS_DIR) $(TEXTURE_REPORT)

# Build presets.pmv archive from cream-of-the-crop dir (only if missing).
$(FULL_PMV_FILE):
	@if [ ! -f "$@" ]; then \
		$(MAKE) presets; \
		mkdir -p $(dir $@); \
		$(GO) run ./cmd/pmv-pack presets $(PRESETS_DIR) $@; \
		echo "=== Built full preset archive $@ ==="; \
	else \
		echo "=== Full preset archive $@ already exists, skipping ==="; \
	fi

pmv: $(FULL_PMV_FILE)

$(PMV_FILE):
	@if [ ! -f "$@" ]; then \
		$(MAKE) presets; \
		mkdir -p $(dir $@); \
		$(GO) run ./cmd/pmv-pack presets $(PRESETS_DIR) $@ $(BENCHMARK_CSV); \
		echo "=== Built filtered portable preset archive $@ ==="; \
	else \
		echo "=== Preset archive $@ already exists, skipping ==="; \
	fi

portable-pmv: $(PMV_FILE)

# Download allmods.zip listing from modland.com (only if missing).
$(ALLMODS_ZIP):
	@mkdir -p $(dir $@)
	curl -fL -o $@ $(ALLMODS_URL)
	@echo "=== Downloaded $(ALLMODS_URL) → $@ ==="

# Build modland catalog from allmods.zip.
$(CATALOG):
	@if [ ! -f "$@" ]; then \
		$(MAKE) $(ALLMODS_ZIP); \
		mkdir -p $(dir $@); \
		$(GO) run ./cmd/modland-catalog/ $(ALLMODS_ZIP) $(DIST_DIR); \
		echo "=== Built catalog $@ ==="; \
	else \
		echo "=== Catalog $@ already exists, skipping ==="; \
	fi

# Validate catalog: build validator in Docker, run with network to test each format.
catalog-validate: $(CATALOG) builder
	@docker rm -f pmv-catalog 2>/dev/null || true
	docker create --name pmv-catalog -v $(PWD):/build -w /build \
		$(DOCKER_BUILDER) bash -c '\
		CGO_ENABLED=1 \
		CGO_CFLAGS="-I/opt/xmp/amd64/include -I/opt/openmpt/amd64/include" \
		CGO_CXXFLAGS="-std=c++11 -I/opt/xmp/amd64/include -I/opt/openmpt/amd64/include" \
		CGO_LDFLAGS="/opt/xmp/amd64/lib/libxmp.a /opt/openmpt/amd64/lib/libopenmpt.a -lstdc++ -lm" \
		go build -o /tmp/validate-catalog \
			-ldflags="-s -w" -buildvcs=false \
			./cmd/validate-catalog/ && \
		/tmp/validate-catalog /build/$(DIST_DIR) && \
		cp /build/$(DIST_DIR)/modland /tmp/modland-catalog'
	docker start -a pmv-catalog
	@mkdir -p $(dir $(CATALOG))
	docker cp pmv-catalog:/tmp/modland-catalog $(CATALOG)
	docker rm -f pmv-catalog

catalog: $(CATALOG) catalog-validate


