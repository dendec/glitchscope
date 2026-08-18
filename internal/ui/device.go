package ui

/*
#cgo LDFLAGS: -lGLESv2
#include <GLES2/gl2.h>
#include "../../lib/projectm/build/src/api/include/projectM-4/version.h"

static const char *glGetStringWrapper(GLenum name) {
	return (const char *)glGetString(name);
}

static const char *projectMVersion() {
	return PROJECTM_VERSION_STRING;
}
*/
import "C"

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"

	"github.com/veandco/go-sdl2/sdl"
)

// DeviceInfo holds hardware and software details for the Device help page.
type DeviceInfo struct {
	OSName    string // e.g. "Ubuntu 24.04.1 LTS"
	Platform  string // SDL platform: "Linux", "Windows", "macOS"
	GoVersion string // e.g. "go1.25.0"
	Arch      string // e.g. "arm64"
	CPUName   string // from /proc/cpuinfo
	TotalRAM  string // e.g. "2.0 GB"

	DisplayName string // e.g. "HDMI-1"
	DisplayW    int    // desktop resolution width
	DisplayH    int    // desktop resolution height
	DDPI        float64
	HDPI        float64
	VDPI        float64

	WindowW, WindowH int // drawable size

	GLVendor   string
	GLRenderer string
	GLVersion  string

	ProjectMVersion string // "4.1.6"
	SDLVersion      string // e.g. "2.30.12"
	SDLRevision     string
	SDLVideoDriver  string // e.g. "wayland"

	AudioBackend    string // e.g. "SDL2"
	AudioSamplerate uint
	AudioChannels   uint
	AudioBuffer     uint
	SoloudVersion   uint
}

// CollectDeviceInfo gathers system, display, GPU, audio, and version info.
// win must have a current GL context.
func CollectDeviceInfo(win *sdl.Window) *DeviceInfo {
	d := &DeviceInfo{
		GoVersion:       runtime.Version(),
		Arch:            runtime.GOARCH,
		ProjectMVersion: C.GoString(C.projectMVersion()),
	}
	d.OSName = readOSName()
	d.CPUName = readCPUName()
	d.TotalRAM = readTotalRAM()
	d.Platform = sdl.GetPlatform()

	if v := sdlVersion(); v != "" {
		d.SDLVersion = v
	}
	d.SDLRevision = sdl.GetRevision()
	if name, err := sdl.GetCurrentVideoDriver(); err == nil {
		d.SDLVideoDriver = name
	}

	if win != nil {
		w, h := win.GLGetDrawableSize()
		d.WindowW, d.WindowH = int(w), int(h)
	}

	fillDisplayInfo(d)
	fillGLInfo(d)
	return d
}

func readOSName() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
		}
	}
	return runtime.GOOS
}

func readCPUName() string {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "model name") {
			return strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
		}
		if strings.HasPrefix(line, "Processor") {
			return strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
		}
	}
	return ""
}

func readTotalRAM() string {
	var info syscall.Sysinfo_t
	err := syscall.Sysinfo(&info)
	if err != nil {
		return ""
	}
	bytes := uint64(info.Totalram) * uint64(info.Unit)
	switch {
	case bytes >= 1<<30:
		return formatFloat(float64(bytes)/float64(1<<30)) + " GB"
	case bytes >= 1<<20:
		return formatFloat(float64(bytes)/float64(1<<20)) + " MB"
	default:
		return formatFloat(float64(bytes)/float64(1<<10)) + " KB"
	}
}

func formatFloat(f float64) string {
	whole := int(f)
	frac := int((f - float64(whole)) * 10)
	if frac == 0 {
		return itoa(whole)
	}
	return fmt.Sprintf("%.1f", f)
}

func sdlVersion() string {
	var v sdl.Version
	sdl.GetVersion(&v)
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

func fillDisplayInfo(d *DeviceInfo) {
	n, err := sdl.GetNumVideoDisplays()
	if err != nil || n == 0 {
		return
	}
	if name, err := sdl.GetDisplayName(0); err == nil {
		d.DisplayName = name
	}
	if rect, err := sdl.GetDisplayBounds(0); err == nil {
		d.DisplayW, d.DisplayH = int(rect.W), int(rect.H)
	}
	if ddpi, hdpi, vdpi, err := sdl.GetDisplayDPI(0); err == nil {
		d.DDPI, d.HDPI, d.VDPI = float64(ddpi), float64(hdpi), float64(vdpi)
	}
}

func fillGLInfo(d *DeviceInfo) {
	if s := glQuery(C.GL_VENDOR); s != "" {
		d.GLVendor = s
	}
	if s := glQuery(C.GL_RENDERER); s != "" {
		d.GLRenderer = s
	}
	if s := glQuery(C.GL_VERSION); s != "" {
		d.GLVersion = s
	}
}

func glQuery(pname C.GLenum) string {
	s := C.glGetStringWrapper(pname)
	if s == nil {
		return ""
	}
	return C.GoString(s)
}

// DeviceInfoLines formats device info as help lines for the Device topic.
func (d *DeviceInfo) DeviceInfoLines() []string {
	if d == nil {
		return []string{"Device information unavailable."}
	}
	lines := []string{
		"**System:**",
		keyVal("OS", d.OSName),
		keyVal("CPU", d.CPUName),
		keyVal("RAM", d.TotalRAM),
		"",
		"**Platform:**",
		keyVal("Runtime", d.GoVersion+" "+d.Arch),
		keyVal("SDL", d.Platform),
		"",
		"**Display:**",
	}
	if d.DisplayName != "" {
		lines = append(lines, keyVal("Monitor", d.DisplayName))
	}
	if d.DisplayW > 0 {
		lines = append(lines, keyVal("Resolution", itoa(d.DisplayW)+"x"+itoa(d.DisplayH)))
	}
	if d.DDPI > 0 {
		lines = append(lines, keyVal("DPI", formatDPI(d.DDPI)))
	}
	lines = append(lines,
		"",
		"**Window:**",
	)
	if d.WindowW > 0 {
		lines = append(lines, keyVal("Size", itoa(d.WindowW)+"x"+itoa(d.WindowH)))
	}
	lines = append(lines,
		"",
		"**GPU:**",
		keyVal("Vendor", d.GLVendor),
		keyVal("Renderer", d.GLRenderer),
		keyVal("GL Version", d.GLVersion),
		"",
		"**Audio:**",
		keyVal("Backend", d.AudioBackend),
	)
	if d.AudioSamplerate > 0 {
		lines = append(lines, keyVal("Sample Rate", itoa(int(d.AudioSamplerate))+" Hz"))
	}
	if d.AudioChannels > 0 {
		lines = append(lines, keyVal("Channels", itoa(int(d.AudioChannels))))
	}
	if d.AudioBuffer > 0 {
		lines = append(lines, keyVal("Buffer", itoa(int(d.AudioBuffer))+" samples"))
	}
	lines = append(lines,
		"",
		"**Versions:**",
		keyVal("projectM", d.ProjectMVersion),
	)
	if d.SDLVersion != "" {
		lines = append(lines, keyVal("SDL", d.SDLVersion+" ("+d.SDLRevision+")"))
	}
	if d.SDLVideoDriver != "" {
		lines = append(lines, keyVal("Video Driver", d.SDLVideoDriver))
	}
	return lines
}

func keyVal(key, val string) string {
	if val == "" {
		return key + ": —"
	}
	return key + ": " + val
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoa(-n)
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func formatDPI(dpi float64) string {
	whole := int(dpi)
	frac := int((dpi - float64(whole)) * 10)
	if frac == 0 {
		return itoa(whole)
	}
	return fmt.Sprintf("%.1f", dpi)
}
