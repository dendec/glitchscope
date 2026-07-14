APP      := mdpp
GO       := go
GOFLAGS  := CGO_ENABLED=1

SDL_CFLAGS := $(shell pkg-config --cflags sdl2)
SDL_LIBS   := $(shell pkg-config --libs sdl2)

PROJECTM_DIR      := lib/projectm
PROJECTM_BUILD    := $(PROJECTM_DIR)/build
PROJECTM_LIB      := $(PROJECTM_BUILD)/src/libprojectM/libprojectM-4.a
PROJECTM_EVAL_LIB := $(PROJECTM_BUILD)/vendor/projectm-eval/projectm-eval/libprojectM_eval.a

CGO_CXXFLAGS := $(SDL_CFLAGS) -Wno-write-strings
CGO_LDFLAGS  := $(SDL_LIBS) $(PROJECTM_LIB) $(PROJECTM_EVAL_LIB) -ldl -lGL -lm

.PHONY: build clean dist-arm64 dist-portmaster run projectm-build

projectm-build: $(PROJECTM_LIB) $(PROJECTM_EVAL_LIB)

$(PROJECTM_LIB) $(PROJECTM_EVAL_LIB): $(PROJECTM_BUILD)/Makefile
	cmake --build $(PROJECTM_BUILD) --target projectM -- -j$$(nproc)

$(PROJECTM_BUILD)/Makefile: $(PROJECTM_DIR)/CMakeLists.txt
	mkdir -p $(PROJECTM_BUILD)
	cd $(PROJECTM_BUILD) && cmake $(PROJECTM_DIR) \
		-DBUILD_SHARED_LIBS=OFF \
		-DENABLE_PLAYLIST=OFF \
		-DENABLE_SDL_UI=OFF \
		-DBUILD_TESTING=OFF \
		-DCMAKE_BUILD_TYPE=Release \
		-DENABLE_INSTALL=OFF

build: projectm-build go.sum
	CGO_ENABLED=1 CGO_CXXFLAGS="$(CGO_CXXFLAGS)" CGO_LDFLAGS="$(CGO_LDFLAGS)" \
		$(GO) build -ldflags="-s -w" -o $(APP) ./cmd/$(APP)

go.sum: go.mod
	$(GO) mod tidy

run: build
	./$(APP)

clean:
	rm -f $(APP) go.sum
	rm -rf dist

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
dist-portmaster: dist-arm64
	@rm -rf dist/portmaster_build
	@mkdir -p dist/portmaster_build/mdpp/presets dist/portmaster_build/mdpp/licenses
	cp portmaster/MDPP.sh dist/portmaster_build/
	cp portmaster/port.json dist/portmaster_build/
	cp portmaster/screenshot.png dist/portmaster_build/ 2>/dev/null; true
	cp dist/mdpp/mdpp dist/portmaster_build/mdpp/
	cp -r dist/mdpp/presets/* dist/portmaster_build/mdpp/presets/ 2>/dev/null; true
	cp portmaster/licenses/* dist/portmaster_build/mdpp/licenses/ 2>/dev/null; true
	cp portmaster/README.md dist/portmaster_build/mdpp/
	cp portmaster/screenshot.png dist/portmaster_build/mdpp/cover.png 2>/dev/null; true
	cp portmaster/loading.png dist/portmaster_build/mdpp/ 2>/dev/null; true
	@RELEASE_DATE=$$(date +%Y%m%d)T000000; \
	printf '<gameList>\n    <game>\n        <path>./MDPP.sh</path>\n        <name>MDPP</name>\n        <desc>MilkDrop Portable Player — plays MP3/FLAC/Ogg with real-time MilkDrop visualizations. Drop your music files into /roms/ports/mdpp/ and enjoy a psychedelic audio experience on your handheld.</desc>\n        <image>./mdpp/cover.png</image>\n        <developer>dendec</developer>\n        <publisher>dendec</publisher>\n        <releasedate>%s</releasedate>\n        <genre>Music</genre>\n    </game>\n</gameList>\n' "$$RELEASE_DATE" > dist/portmaster_build/mdpp/gameinfo.xml
	@rm -f dist/mdpp.zip
	cd dist/portmaster_build && zip -r ../mdpp.zip "MDPP.sh" port.json screenshot.png mdpp
	@echo "=== Generated dist/mdpp.zip ==="
	@ls -lh dist/mdpp.zip

deploy: dist-arm64
	adb shell "mkdir -p $(DEVICE_DIR)"
	adb push dist/mdpp/mdpp $(DEVICE_DIR)/
	adb push portmaster/MDPP.sh $(PORTS_DIR)/
	adb push portmaster/loading.png $(DEVICE_DIR)/ 2>/dev/null || true
	adb push test_data/song.mp3 $(DEVICE_DIR)/song.mp3
	adb shell "killall -9 mdpp 2>/dev/null; true"
	@echo "=== Fast deployed binary and song ==="

deploy-portmaster: dist-portmaster
	adb push dist/mdpp.zip $(PM_AUTOINSTALL)/
	@echo "=== Deployed to autoinstall ==="
	adb shell "mkdir -p $(DEVICE_DIR)"
	adb push test_data/song.mp3 $(DEVICE_DIR)/song.mp3
	@echo "=== Song deployed to $(DEVICE_DIR) ==="
