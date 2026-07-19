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

SDL_CFLAGS := $(shell pkg-config --cflags sdl2)
SDL_LIBS   := $(shell pkg-config --libs sdl2)

PROJECTM_DIR      := lib/projectm
PROJECTM_BUILD    := $(PROJECTM_DIR)/build
PROJECTM_LIB      := $(PROJECTM_BUILD)/src/libprojectM/libprojectM-4.a
PROJECTM_EVAL_LIB := $(PROJECTM_BUILD)/vendor/projectm-eval/projectm-eval/libprojectM_eval.a
PROJECTM_PATCH    := patches/projectm-feedback.patch

CGO_CXXFLAGS := $(SDL_CFLAGS) -Wno-write-strings
CGO_LDFLAGS  := $(SDL_LIBS) $(PROJECTM_LIB) $(PROJECTM_EVAL_LIB) -ldl -lGL -lGLESv2 -lm -lopenmpt

DOCKER_IMAGE_X64 := mdpp-builder

PRESETS_REPO  := https://github.com/projectM-visualizer/presets-cream-of-the-crop.git
PRESETS_DIR   := dist/presets-cream-of-the-crop
MDP_FILE      := dist/presets.mdp
FULL_MDP_FILE := dist/presets-all.mdp
BENCHMARK_CSV := docs/benchmark/render-scale-0.5_mesh-8.csv

TEXTURES_REPO := https://github.com/projectM-visualizer/presets-milkdrop-texture-pack.git
TEXTURES_DIR  := dist/presets-milkdrop-texture-pack

.PHONY: build clean dist dist-arm64 dist-portmaster lint run run-local projectm-build submodules test tidy presets mdp portable-mdp textures

submodules:
	git submodule update --init --recursive

projectm-build: submodules $(PROJECTM_LIB) $(PROJECTM_EVAL_LIB)

$(PROJECTM_LIB) $(PROJECTM_EVAL_LIB): $(PROJECTM_BUILD)/Makefile
	cmake --build $(PROJECTM_BUILD) --target projectM -- -j$$(nproc)

$(PROJECTM_BUILD)/Makefile: $(PROJECTM_DIR)/CMakeLists.txt $(PROJECTM_PATCH)
	@# Apply custom patch for feedback framebuffer injection.
	cd $(PROJECTM_DIR) && git apply ../../$(PROJECTM_PATCH) 2>/dev/null; true
	@# Patch config.h.cmake.in — upstream uses git hash but we
	@# build in detached/submodule mode without full git history.
	sed -i 's/#cmakedefine PROJECTM_VERSION_VCS @PROJECTM_VERSION_VCS@/#define PROJECTM_VERSION_VCS "Unknown"/' \
		$(PROJECTM_DIR)/config.h.cmake.in 2>/dev/null; true
	mkdir -p $(PROJECTM_BUILD)
	cd $(PROJECTM_BUILD) && cmake .. \
		-DBUILD_SHARED_LIBS=OFF \
		-DENABLE_PLAYLIST=OFF \
		-DENABLE_SDL_UI=OFF \
		-DBUILD_TESTING=OFF \
		-DCMAKE_BUILD_TYPE=Release \
		-DENABLE_INSTALL=OFF

$(FONT_SUBSET): $(FONT_RANGES)
	wget -q -O /tmp/unifont-full.otf $(FONT_URL)
	pyftsubset /tmp/unifont-full.otf \
		--unicodes=$$(python3 -c "import json; print(','.join(json.load(open('$(FONT_RANGES)'))))") \
		--no-prune-unicode-ranges \
		--no-subset-tables+=OS/2 \
		--output-file=$(FONT_SUBSET) 2>&1
	python3 -c "from fontTools.ttLib import TTFont; f=TTFont('$(FONT_SUBSET)'); c={cp for t in f['cmap'].tables for cp in t.cmap}; r='$(FONT_REQUIRED)'.split(','); m=[x for x in r if int(x[2:],16) not in c]; assert not m, 'missing UI glyphs: '+','.join(m)"
	rm -f /tmp/unifont-full.otf

# Docker build (CI/packaging).  Depends on mdp so the preset archive is ready.
dist: $(MDP_FILE)
	docker build -t $(DOCKER_IMAGE_X64) -f Dockerfile .
	@rm -rf $(X64_DIST_DIR)
	@mkdir -p $(X64_DIST_DIR)
	@docker rm -f mdpp-extract-x64 2>/dev/null || true
	docker create --name mdpp-extract-x64 $(DOCKER_IMAGE_X64)
	docker cp mdpp-extract-x64:/dist/mdpp/. $(X64_DIST_DIR)/
	docker rm mdpp-extract-x64
	@# Replace test presets with the real .mdp archive.
	rm -rf $(X64_DIST_DIR)/presets/*
	cp $(MDP_FILE) $(X64_DIST_DIR)/presets/.mdp
	@echo "=== Built $(APP) ==="
	@ls -lhR $(X64_DIST_DIR)/

# Local build (default for dev, needs all deps installed).
build: $(FONT_SUBSET) projectm-build
	@rm -rf $(LOCAL_DIST_DIR)
	@mkdir -p $(LOCAL_DIST_DIR)
	CGO_ENABLED=1 CGO_CFLAGS="$(CGO_CFLAGS)" CGO_CXXFLAGS="$(CGO_CXXFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
		$(GO) build -ldflags="-s -w" -o $(LOCAL_DIST_DIR)/$(APP) ./cmd/$(APP)

# Run locally with presets + textures + test music (run `make build` first).
run-local: mdp textures
	@mkdir -p dist/presets dist/textures dist/music
	cp $(MDP_FILE) dist/presets/.mdp
	cp -r $(TEXTURES_DIR)/textures/* dist/textures/ 2>/dev/null; true
	cp test_data/music/* dist/music/ 2>/dev/null; true
	./$(LOCAL_DIST_DIR)/$(APP)

# Run via Docker with X11 + audio + music mount.
run: dist textures
	@mkdir -p music
	docker run --rm -it \
		--net=host \
		-e DISPLAY=$${DISPLAY} \
		-v /tmp/.X11-unix:/tmp/.X11-unix:ro \
		-v /dev/snd:/dev/snd:ro \
		-v "$$(pwd)/music:/build/test_data/music:ro" \
		-v "$$(pwd)/$(TEXTURES_DIR)/textures:/dist/mdpp/textures:ro" \
		--group-add audio \
		$(DOCKER_IMAGE_X64)

lint:
	@which golangci-lint >/dev/null 2>&1 || go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	CGO_ENABLED=1 CGO_CFLAGS="$(CGO_CFLAGS)" CGO_CXXFLAGS="$(CGO_CXXFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
		golangci-lint run ./cmd/... ./internal/...

test:
	CGO_ENABLED=1 CGO_CFLAGS="$(CGO_CFLAGS)" CGO_CXXFLAGS="$(CGO_CXXFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
		$(GO) test -count=1 ./cmd/... ./internal/...

tidy:
	$(GO) mod tidy

clean:
	rm -f $(APP) go.sum
	rm -rf dist

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

# Build presets.mdp archive from cream-of-the-crop dir (only if missing).
$(FULL_MDP_FILE):
	$(GO) run ./cmd/mdp-pack $(PRESETS_DIR) $@
	@echo "=== Built full preset archive ==="
	@ls -lh $@

mdp: presets $(FULL_MDP_FILE)

$(MDP_FILE): presets $(BENCHMARK_CSV) cmd/mdp-pack/main.go
	$(GO) run ./cmd/mdp-pack $(PRESETS_DIR) $@ $(BENCHMARK_CSV)
	@echo "=== Built filtered portable preset archive ==="
	@ls -lh $@

portable-mdp: presets $(MDP_FILE)

# ARM64 cross-build via Docker.
DOCKER_IMAGE := mdpp-arm64
DOCKER_FILE  := Dockerfile.arm64
PORTS_DIR    := /userdata/roms/ports
DEVICE_DIR   := $(PORTS_DIR)/mdpp
PM_AUTOINSTALL := /userdata/system/.local/share/PortMaster/autoinstall

dist-arm64: textures
	docker build -t $(DOCKER_IMAGE) -f $(DOCKER_FILE) .
	@rm -rf $(ARM64_DIST_DIR)
	@mkdir -p $(ARM64_DIST_DIR)
	@docker rm -f mdpp-extract 2>/dev/null || true
	docker create --name mdpp-extract $(DOCKER_IMAGE)
	docker cp mdpp-extract:/dist/. $(ARM64_DIST_DIR)/
	docker rm mdpp-extract
	@echo "=== $(ARM64_DIST_DIR)/ ==="
	@ls -lhR $(ARM64_DIST_DIR)/

# PortMaster packaging — structure must match zimlite (gameinfo.xml, README.md at root).
dist-portmaster: dist-arm64 portable-mdp textures
	@rm -rf dist/portmaster_build
	@mkdir -p dist/portmaster_build/mdpp/presets dist/portmaster_build/mdpp/textures dist/portmaster_build/mdpp/licenses
	cp portmaster/MDPP.sh dist/portmaster_build/
	cp portmaster/port.json dist/portmaster_build/
	cp portmaster/README.md dist/portmaster_build/
	cp portmaster/screenshot.png dist/portmaster_build/
	cp portmaster/gameinfo.xml dist/portmaster_build/ 2>/dev/null; true
	@RELEASE_DATE=$$(date +%Y%m%d)T000000; \
	printf '<gameList>\n    <game>\n        <path>./MDPP.sh</path>\n        <name>MDPP</name>\n        <desc>MilkDrop Portable Player — plays MP3/FLAC/Ogg/Mod/XM/IT/S3M with real-time MilkDrop visualizations. Drop your music into /roms/ports/mdpp/music/ and enjoy a psychedelic audio experience on your handheld.</desc>\n        <image>./mdpp/cover.png</image>\n        <developer>dendec</developer>\n        <publisher>dendec</publisher>\n        <releasedate>%s</releasedate>\n        <genre>Music</genre>\n    </game>\n</gameList>\n' "$$RELEASE_DATE" > dist/portmaster_build/mdpp/gameinfo.xml
	cp $(ARM64_DIST_DIR)/mdpp/mdpp dist/portmaster_build/mdpp/
	cp $(MDP_FILE) dist/portmaster_build/mdpp/presets/.mdp
	cp $(TEXTURES_DIR)/textures/* dist/portmaster_build/mdpp/textures/ 2>/dev/null; true
	cp portmaster/licenses/* dist/portmaster_build/mdpp/licenses/ 2>/dev/null; true
	cp portmaster/screenshot.png dist/portmaster_build/mdpp/cover.png 2>/dev/null; true
	@rm -f dist/mdpp.zip
	cd dist/portmaster_build && zip -r ../mdpp.zip "MDPP.sh" README.md gameinfo.xml port.json screenshot.png mdpp
	@echo "=== Generated dist/mdpp.zip ==="
	@ls -lh dist/mdpp.zip

deploy: dist-arm64 portable-mdp textures
	adb shell "mkdir -p $(DEVICE_DIR)"
	adb push $(ARM64_DIST_DIR)/mdpp/mdpp $(DEVICE_DIR)/
	adb push portmaster/MDPP.sh $(PORTS_DIR)/
	adb shell "mkdir -p $(DEVICE_DIR)/music"
	adb push test_data/music/* $(DEVICE_DIR)/music/
	# Deploy presets as single .mdp archive (fast on FAT32).
	adb shell "mkdir -p $(DEVICE_DIR)/presets"
	adb push $(MDP_FILE) $(DEVICE_DIR)/presets/.mdp
	# Deploy textures for milkdrop presets with user textures.
	adb shell "mkdir -p $(DEVICE_DIR)/textures"
	adb push $(TEXTURES_DIR)/textures/* $(DEVICE_DIR)/textures/
	adb shell "killall -9 mdpp 2>/dev/null; true"
	@echo "=== Deployed binary + music + presets + textures ==="

deploy-song:
	adb shell "mkdir -p $(DEVICE_DIR)/music"
	adb push test_data/song.mp3 $(DEVICE_DIR)/
	adb push test_data/music $(DEVICE_DIR)/music
	@echo "=== Deployed song + music test dirs ==="

deploy-portmaster: dist-portmaster
	adb push dist/mdpp.zip $(PM_AUTOINSTALL)/
	@echo "=== Deployed to autoinstall ==="
	adb shell "mkdir -p $(DEVICE_DIR)"
	adb push test_data/song.mp3 $(DEVICE_DIR)/song.mp3
	@echo "=== Song deployed to $(DEVICE_DIR) ==="

kill:
	adb shell "killall -9 mdpp 2>/dev/null || true"
	@echo "=== Killed mdpp on device ==="
