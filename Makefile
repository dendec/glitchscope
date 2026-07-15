APP      := mdpp
FONT_SUBSET := internal/ui/assets/unifont.otf
FONT_RANGES := internal/ui/font_ranges.json
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

.PHONY: build clean dist dist-arm64 dist-portmaster lint run run-local projectm-build submodules test tidy presets mdp

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

$(FONT_SUBSET):
	wget -q -O /tmp/unifont-full.otf $(FONT_URL)
	pyftsubset /tmp/unifont-full.otf \
		--unicodes=$$(python3 -c "import json; print(','.join(json.load(open('$(FONT_RANGES)'))))") \
		--no-subset-tables+=OS/2 \
		--output-file=$(FONT_SUBSET) 2>&1
	rm -f /tmp/unifont-full.otf

# Docker build (CI/packaging).
dist:
	docker build -t $(DOCKER_IMAGE_X64) -f Dockerfile .
	@mkdir -p dist
	@docker rm -f mdpp-extract-x64 2>/dev/null || true
	docker create --name mdpp-extract-x64 $(DOCKER_IMAGE_X64)
	docker cp mdpp-extract-x64:/dist/mdpp/mdpp ./$(APP)
	docker rm mdpp-extract-x64
	@echo "=== Built $(APP) ==="
	@ls -lh $(APP)

# Local build (default for dev, needs all deps installed).
build: $(FONT_SUBSET) projectm-build
	@rm -rf dist/$(APP)
	@mkdir -p dist
	CGO_ENABLED=1 CGO_CFLAGS="$(CGO_CFLAGS)" CGO_CXXFLAGS="$(CGO_CXXFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
		$(GO) build -ldflags="-s -w" -o dist/$(APP) ./cmd/$(APP)

# Run locally with presets + test music (run `make build` first).
run-local: mdp
	@mkdir -p dist/presets dist/music
	cp $(MDP_FILE) dist/presets/.mdp
	cp test_data/music/* dist/music/ 2>/dev/null; true
	./dist/mdpp

# Run via Docker with X11 + audio + music mount.
run: dist
	@mkdir -p music
	docker run --rm -it \
		--net=host \
		-e DISPLAY=$${DISPLAY} \
		-v /tmp/.X11-unix:/tmp/.X11-unix:ro \
		-v /dev/snd:/dev/snd:ro \
		-v "$$(pwd)/music:/build/test_data/music:ro" \
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

# Build presets.mdp archive from cream-of-the-crop dir (only if missing).
$(MDP_FILE):
	$(GO) run ./cmd/mdp-pack $(PRESETS_DIR) $@
	@echo "=== Built $@ ==="
	@ls -lh $@

mdp: presets $(MDP_FILE)

# ARM64 cross-build via Docker.
DOCKER_IMAGE := mdpp-arm64
DOCKER_FILE  := Dockerfile.arm64
PORTS_DIR    := /userdata/roms/ports
DEVICE_DIR   := $(PORTS_DIR)/mdpp
PM_AUTOINSTALL := /userdata/system/.local/share/PortMaster/autoinstall

dist-arm64:
	docker build -t $(DOCKER_IMAGE) -f $(DOCKER_FILE) .
	@mkdir -p dist
	@docker rm -f mdpp-extract 2>/dev/null || true
	docker create --name mdpp-extract $(DOCKER_IMAGE)
	docker cp mdpp-extract:/dist/. dist/
	docker rm mdpp-extract
	@echo "=== dist/ ==="
	@ls -lhR dist/

# PortMaster packaging.
dist-portmaster: dist-arm64 mdp
	@rm -rf dist/portmaster_build
	@mkdir -p dist/portmaster_build/mdpp/presets dist/portmaster_build/mdpp/licenses
	cp portmaster/MDPP.sh dist/portmaster_build/
	cp portmaster/port.json dist/portmaster_build/
	cp portmaster/screenshot.png dist/portmaster_build/ 2>/dev/null; true
	cp dist/mdpp/mdpp dist/portmaster_build/mdpp/
	cp $(MDP_FILE) dist/portmaster_build/mdpp/presets/.mdp
	cp portmaster/licenses/* dist/portmaster_build/mdpp/licenses/ 2>/dev/null; true
	cp portmaster/README.md dist/portmaster_build/mdpp/
	cp portmaster/screenshot.png dist/portmaster_build/mdpp/cover.png 2>/dev/null; true
	@RELEASE_DATE=$$(date +%Y%m%d)T000000; \
	printf '<gameList>\n    <game>\n        <path>./MDPP.sh</path>\n        <name>MDPP</name>\n        <desc>MilkDrop Portable Player — plays MP3/FLAC/Ogg/Mod/XM/IT/S3M with real-time MilkDrop visualizations. Drop your music into /roms/ports/mdpp/music/ and enjoy a psychedelic audio experience on your handheld.</desc>\n        <image>./mdpp/cover.png</image>\n        <developer>dendec</developer>\n        <publisher>dendec</publisher>\n        <releasedate>%s</releasedate>\n        <genre>Music</genre>\n    </game>\n</gameList>\n' "$$RELEASE_DATE" > dist/portmaster_build/mdpp/gameinfo.xml
	@rm -f dist/mdpp.zip
	cd dist/portmaster_build && zip -r ../mdpp.zip "MDPP.sh" port.json screenshot.png mdpp
	@echo "=== Generated dist/mdpp.zip ==="
	@ls -lh dist/mdpp.zip

deploy: dist-arm64 mdp
	adb shell "mkdir -p $(DEVICE_DIR)"
	adb push dist/mdpp/mdpp $(DEVICE_DIR)/
	adb push portmaster/MDPP.sh $(PORTS_DIR)/
	adb shell "mkdir -p $(DEVICE_DIR)/music"
	adb push test_data/music/* $(DEVICE_DIR)/music/
	# Deploy presets as single .mdp archive (fast on FAT32).
	adb shell "mkdir -p $(DEVICE_DIR)/presets"
	adb push $(MDP_FILE) $(DEVICE_DIR)/presets/.mdp
	adb shell "killall -9 mdpp 2>/dev/null; true"
	@echo "=== Deployed binary + music + presets ==="

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
