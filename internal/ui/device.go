package ui

/*
#cgo LDFLAGS: -lGLESv2
#cgo CFLAGS: -I/opt/projectm/include
#include <GLES2/gl2.h>
#include <projectM-4/version.h>

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
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/veandco/go-sdl2/sdl"
)

// DeviceInfo holds hardware and software details for the Device help page.
type DeviceInfo struct {
	DeviceName string // handheld model, e.g. "TrimUI Smart Pro"
	CFWName    string // custom firmware, e.g. "muOS"
	CFWVersion string
	PMVersion  string
	SoC        string // chipset name supplied by PortMaster or device tree

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
	d.fillHandheldInfo(os.Getenv, os.ReadFile)
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

type readFileFunc func(string) ([]byte, error)

func (d *DeviceInfo) fillHandheldInfo(getenv func(string) string, readFile readFileFunc) {
	d.DeviceName = cleanDeviceValue(getenv("DEVICE_NAME"))
	d.SoC = cleanDeviceValue(getenv("DEVICE_CPU"))
	d.CFWName = cleanDeviceValue(getenv("CFW_NAME"))
	d.CFWVersion = cleanDeviceValue(getenv("CFW_VERSION"))
	d.PMVersion = cleanDeviceValue(getenv("PM_VERSION"))

	osRelease := readKeyValues(readFile, "/etc/os-release")
	if d.CFWName == "" {
		d.CFWName = osRelease["OS_NAME"]
	}
	if d.CFWVersion == "" {
		d.CFWVersion = osRelease["OS_VERSION"]
	}
	if d.DeviceName == "" {
		paths := []string{
			"/opt/muos/device/config/board/name",
			"/opt/muos/config/device.txt",
			"/boot/boot/knulli.board",
			"/boot/boot/batocera.board",
			"/boot/boot/system.board",
			"/sys/firmware/devicetree/base/model",
		}
		if home := cleanDeviceValue(getenv("HOME")); home != "" {
			paths = append([]string{
				filepath.Join(home, ".config", ".CUSTOM_DEVICE"),
				filepath.Join(home, ".config", ".DEVICE"),
				filepath.Join(home, ".config", ".OS_ARCH"),
			}, paths...)
		}
		d.DeviceName = firstReadableLine(readFile, paths...)
	}
	if d.DeviceName == "" && strings.EqualFold(d.CFWName, "TrimUI") {
		d.DeviceName = "TrimUI Smart Pro"
	}
	if d.SoC == "" {
		d.SoC = firstReadableLine(readFile,
			"/sys/devices/soc0/soc_id",
			"/sys/devices/soc0/machine",
		)
	}
	if d.PMVersion == "" {
		d.PMVersion = firstReadableLine(readFile,
			"/roms/ports/PortMaster/version",
			"/opt/system/Tools/PortMaster/version",
		)
	}
}

func readKeyValues(readFile readFileFunc, path string) map[string]string {
	values := make(map[string]string)
	data, err := readFile(path)
	if err != nil {
		return values
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = cleanDeviceValue(value)
		}
	}
	return values
}

func firstReadableLine(readFile readFileFunc, paths ...string) string {
	for _, path := range paths {
		data, err := readFile(path)
		if err == nil {
			if value := cleanDeviceValue(string(data)); value != "" {
				return value
			}
		}
	}
	return ""
}

func cleanDeviceValue(value string) string {
	return strings.Trim(value, "\" \t\r\n\x00")
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
		if strings.HasPrefix(line, "Hardware") {
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
		"**Device:**",
	}
	if d.DeviceName != "" {
		lines = append(lines, "**"+d.DeviceName+"**")
	}
	if d.CFWName != "" {
		lines = append(lines, keyVal("Firmware", strings.TrimSpace(d.CFWName+" "+d.CFWVersion)))
	}
	if d.SoC != "" {
		lines = append(lines, keyVal("SoC", d.SoC))
	}
	lines = append(lines,
		keyVal("OS", d.OSName),
		keyVal("CPU", d.CPUName),
		keyVal("RAM", d.TotalRAM),
		"",
		"**Software:**",
		keyVal("Runtime", d.GoVersion+" "+d.Arch),
	)
	if d.PMVersion != "" {
		lines = append(lines, keyVal("PortMaster", d.PMVersion))
	}
	lines = append(lines, "", "**Display:**")
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
