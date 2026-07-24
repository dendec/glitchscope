APP      := mdpp
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
MDP_FILE           := dist/presets.mdp
FULL_MDP_FILE      := dist/presets-all.mdp
BENCHMARK_CSV      := docs/benchmark/render-scale-0.5_mesh-8.csv
TEXTURES_REPO      := https://github.com/projectM-visualizer/presets-milkdrop-texture-pack.git
TEXTURES_DIR       := dist/presets-milkdrop-texture-pack
OPTIMIZED_TEXTURES := dist/textures-optimized
TEXTURES_MDP_FILE  := dist/textures.mdp
TEXTURE_REPORT     := docs/texture-usage-report.csv
PORTS_DIR          := /userdata/roms/ports
DEVICE_DIR         := $(PORTS_DIR)/mdpp
PM_AUTOINSTALL     := /userdata/system/.local/share/PortMaster/autoinstall

SDL_CFLAGS := $(shell pkg-config --cflags sdl2 2>/dev/null)
SDL_LIBS   := $(shell pkg-config --libs sdl2 2>/dev/null)

PROJECTM_DIR      := lib/projectm
PROJECTM_BUILD    := $(PROJECTM_DIR)/build
PROJECTM_LIB      := $(PROJECTM_BUILD)/src/libprojectM/libprojectM-4.a
PROJECTM_EVAL_LIB := $(PROJECTM_BUILD)/vendor/projectm-eval/projectm-eval/libprojectM_eval.a
PROJECTM_PATCH    := patches/projectm-feedback.patch

CGO_CXXFLAGS := $(SDL_CFLAGS) -Wno-write-strings
CGO_LDFLAGS  := $(SDL_LIBS) $(PROJECTM_LIB) $(PROJECTM_EVAL_LIB) -ldl -lGL -lGLESv2 -lm -lopenmpt

DOCKER_IMAGE_X64 := mdpp-builder
DOCKER_BUILDER   := mdpp-builder:latest

.PHONY: builder build clean dist dist-arm64 dist-portmaster lint run run-local projectm-build submodules test tidy presets mdp portable-mdp textures optimize-textures texture-archive texture-report

# Builder image: C/C++ static dependencies compiled once for amd64 & arm64
builder:
	docker build -t $(DOCKER_BUILDER) -f Dockerfile.builder .

# Docker build (amd64)
dist: builder $(MDP_FILE) $(TEXTURES_MDP_FILE)
	docker build --build-arg BUILDER_IMAGE=$(DOCKER_BUILDER) --build-arg TARGETARCH=amd64 -t mdpp:amd64 -f Dockerfile .
	@rm -rf $(X64_DIST_DIR)
	@mkdir -p $(X64_DIST_DIR)
	@docker rm -f mdpp-extract-x64 2>/dev/null || true
	docker create --name mdpp-extract-x64 mdpp:amd64
	docker cp mdpp-extract-x64:/dist/mdpp/. $(X64_DIST_DIR)/
	docker rm mdpp-extract-x64
	@# Replace test presets with the real .mdp archive.
	rm -rf $(X64_DIST_DIR)/presets/*
	cp $(MDP_FILE) $(X64_DIST_DIR)/presets/presets.mdp
	rm -rf $(X64_DIST_DIR)/textures
	cp $(TEXTURES_MDP_FILE) $(X64_DIST_DIR)/presets/textures.mdp
	@echo "=== Built $(APP) (amd64) ==="
	@ls -lhR $(X64_DIST_DIR)/

# ARM64 cross-build via Docker
DOCKER_IMAGE_ARM64 := mdpp:arm64

dist-arm64: builder portable-mdp $(TEXTURES_MDP_FILE)
	docker build --build-arg BUILDER_IMAGE=$(DOCKER_BUILDER) --build-arg TARGETARCH=arm64 -t $(DOCKER_IMAGE_ARM64) -f Dockerfile .
	@rm -rf $(ARM64_DIST_DIR)
	@mkdir -p $(ARM64_DIST_DIR)
	@docker rm -f mdpp-extract 2>/dev/null || true
	docker create --name mdpp-extract $(DOCKER_IMAGE_ARM64)
	docker cp mdpp-extract:/dist/. $(ARM64_DIST_DIR)/
	docker rm mdpp-extract
	rm -rf $(ARM64_DIST_DIR)/mdpp/presets/*
	cp $(MDP_FILE) $(ARM64_DIST_DIR)/mdpp/presets/presets.mdp
	rm -rf $(ARM64_DIST_DIR)/mdpp/textures
	cp $(TEXTURES_MDP_FILE) $(ARM64_DIST_DIR)/mdpp/presets/textures.mdp
	@echo "=== $(ARM64_DIST_DIR)/ ==="
	@ls -lhR $(ARM64_DIST_DIR)/

# PortMaster packaging — structure must match zimlite (gameinfo.xml, README.md at root).
dist-portmaster: dist-arm64 portable-mdp $(TEXTURES_MDP_FILE)
	@rm -rf dist/portmaster_build
	@mkdir -p dist/portmaster_build/mdpp/presets dist/portmaster_build/mdpp/licenses
	cp portmaster/MDPP.sh dist/portmaster_build/
	cp portmaster/port.json dist/portmaster_build/
	cp portmaster/README.md dist/portmaster_build/
	cp portmaster/screenshot.png dist/portmaster_build/
	cp portmaster/gameinfo.xml dist/portmaster_build/ 2>/dev/null; true
	@RELEASE_DATE=$$(date +%Y%m%d)T000000; \
	printf '<gameList>\n    <game>\n        <path>./MDPP.sh</path>\n        <name>MDPP</name>\n        <desc>MilkDrop Portable Player — plays MP3/FLAC/Ogg/Mod/XM/IT/S3M with real-time MilkDrop visualizations. Drop your music into /roms/ports/mdpp/music/ and enjoy a psychedelic audio experience on your handheld.</desc>\n        <image>./mdpp/cover.png</image>\n        <developer>dendec</developer>\n        <publisher>dendec</publisher>\n        <releasedate>%s</releasedate>\n        <genre>Music</genre>\n    </game>\n</gameList>\n' "$$RELEASE_DATE" > dist/portmaster_build/mdpp/gameinfo.xml
	cp $(ARM64_DIST_DIR)/mdpp/mdpp dist/portmaster_build/mdpp/
	cp $(MDP_FILE) dist/portmaster_build/mdpp/presets/presets.mdp
	cp $(TEXTURES_MDP_FILE) dist/portmaster_build/mdpp/presets/textures.mdp
	cp portmaster/licenses/* dist/portmaster_build/mdpp/licenses/ 2>/dev/null; true
	cp portmaster/screenshot.png dist/portmaster_build/mdpp/cover.png 2>/dev/null; true
	@rm -f dist/mdpp.zip
	cd dist/portmaster_build && zip -r ../mdpp.zip "MDPP.sh" README.md gameinfo.xml port.json screenshot.png mdpp
	@echo "=== Generated dist/mdpp.zip ==="
	@ls -lh dist/mdpp.zip

deploy: dist-arm64 portable-mdp $(TEXTURES_MDP_FILE)
	adb shell "mkdir -p $(DEVICE_DIR)"
	adb push $(ARM64_DIST_DIR)/mdpp/mdpp $(DEVICE_DIR)/
	adb push portmaster/MDPP.sh $(PORTS_DIR)/
	adb shell "mkdir -p $(DEVICE_DIR)/music"
	adb push test_data/* $(DEVICE_DIR)/music
	# Deploy presets as single .mdp archive (fast on FAT32).
	adb shell "mkdir -p $(DEVICE_DIR)/presets"
	adb push $(MDP_FILE) $(DEVICE_DIR)/presets/presets.mdp
	# Deploy the optimized texture archive for MilkDrop presets.
	adb push $(TEXTURES_MDP_FILE) $(DEVICE_DIR)/presets/textures.mdp
	adb shell "killall -9 mdpp 2>/dev/null; true"
	@echo "=== Deployed binary + music + presets + textures ==="

deploy-portmaster: dist-portmaster
	adb push dist/mdpp.zip $(PM_AUTOINSTALL)/
	@echo "=== Deployed to autoinstall ==="
	adb shell "mkdir -p $(DEVICE_DIR)/music"
	adb push test_data/* $(DEVICE_DIR)/music
	@echo "=== Song deployed to $(DEVICE_DIR) ==="

kill:
	adb shell "killall -9 mdpp 2>/dev/null || true"
	@echo "=== Killed mdpp on device ==="

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
	@rm -f $(TEXTURES_MDP_FILE)
	$(MAKE) $(TEXTURES_MDP_FILE)

$(TEXTURES_MDP_FILE):
	@if [ ! -f "$@" ]; then \
		mkdir -p $(dir $@); \
		if [ ! -d "$(OPTIMIZED_TEXTURES)" ]; then \
			$(MAKE) optimize-textures; \
		fi; \
		$(GO) run ./cmd/mdp-pack textures $(OPTIMIZED_TEXTURES) $@; \
		echo "=== Built texture archive $@ ==="; \
	else \
		echo "=== Texture archive $@ already exists, skipping ==="; \
	fi

texture-report: presets cmd/texture-report/main.go
	go run ./cmd/texture-report $(PRESETS_DIR) $(TEXTURE_REPORT)

# Build presets.mdp archive from cream-of-the-crop dir (only if missing).
$(FULL_MDP_FILE):
	@if [ ! -f "$@" ]; then \
		$(MAKE) presets; \
		mkdir -p $(dir $@); \
		$(GO) run ./cmd/mdp-pack presets $(PRESETS_DIR) $@; \
		echo "=== Built full preset archive $@ ==="; \
	else \
		echo "=== Full preset archive $@ already exists, skipping ==="; \
	fi

mdp: $(FULL_MDP_FILE)

$(MDP_FILE):
	@if [ ! -f "$@" ]; then \
		$(MAKE) presets; \
		mkdir -p $(dir $@); \
		$(GO) run ./cmd/mdp-pack presets $(PRESETS_DIR) $@ $(BENCHMARK_CSV); \
		echo "=== Built filtered portable preset archive $@ ==="; \
	else \
		echo "=== Preset archive $@ already exists, skipping ==="; \
	fi

portable-mdp: $(MDP_FILE)


