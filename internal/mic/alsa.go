package mic

/*
#cgo LDFLAGS: -lasound
#include <alsa/asoundlib.h>
#include <stdlib.h>

// pmv_alsa_open configures a nonblocking capture PCM: interleaved, float32
// (falling back to S16), 1-2 channels, 48kHz. Returns 0 on success and
// writes the negotiated format flag (*fmt, 1=float32), channels and rate.
static int pmv_alsa_open(snd_pcm_t **pcm, const char *name, int *fmt, int *channels, unsigned int *rate) {
    int rc = snd_pcm_open(pcm, name, SND_PCM_STREAM_CAPTURE, SND_PCM_NONBLOCK);
    if (rc < 0) return rc;
    snd_pcm_hw_params_t *hw;
    snd_pcm_hw_params_alloca(&hw);
    if ((rc = snd_pcm_hw_params_any(*pcm, hw)) < 0) return rc;
    if ((rc = snd_pcm_hw_params_set_access(*pcm, hw, SND_PCM_ACCESS_RW_INTERLEAVED)) < 0) return rc;
    snd_pcm_format_t format = SND_PCM_FORMAT_FLOAT_LE;
    if (snd_pcm_hw_params_set_format(*pcm, hw, format) < 0) {
        format = SND_PCM_FORMAT_S16_LE;
        if ((rc = snd_pcm_hw_params_set_format(*pcm, hw, format)) < 0) return rc;
    }
    *fmt = (format == SND_PCM_FORMAT_FLOAT_LE) ? 1 : 0;
    if (snd_pcm_hw_params_set_channels(*pcm, hw, 1) < 0) {
        if ((rc = snd_pcm_hw_params_set_channels(*pcm, hw, 2)) < 0) return rc;
    }
    if ((rc = snd_pcm_hw_params_get_channels(hw, (unsigned int *)channels)) < 0) return rc;
    *rate = 48000;
    if ((rc = snd_pcm_hw_params_set_rate_near(*pcm, hw, rate, 0)) < 0) return rc;
    snd_pcm_uframes_t period = 512;
    if ((rc = snd_pcm_hw_params_set_period_size_near(*pcm, hw, &period, 0)) < 0) return rc;
    unsigned int periods = 2;
    if ((rc = snd_pcm_hw_params_set_periods_min(*pcm, hw, &periods, 0)) < 0) return rc;
    if ((rc = snd_pcm_hw_params(*pcm, hw)) < 0) return rc;
    snd_pcm_sw_params_t *sw;
    snd_pcm_sw_params_alloca(&sw);
    if ((rc = snd_pcm_sw_params_current(*pcm, sw)) < 0) return rc;
    if ((rc = snd_pcm_sw_params_set_avail_min(*pcm, sw, period)) < 0) return rc;
    if ((rc = snd_pcm_sw_params_set_start_threshold(*pcm, sw, 1)) < 0) return rc;
    if ((rc = snd_pcm_sw_params(*pcm, sw)) < 0) return rc;
    return 0;
}

// pmv_alsa_read reads up to frames frames. Returns frames read (>=0),
// or a negative errno (recovered -EPIPE/-ESTRPIPE internally). -EAGAIN is
// reported as 0 (no data yet).
static int pmv_alsa_read(snd_pcm_t *pcm, void *buf, int frames) {
    int rc;
    do {
        rc = snd_pcm_readi(pcm, buf, (snd_pcm_uframes_t)frames);
    } while (rc == -EINTR);
    if (rc < 0 && rc != -EAGAIN) {
        if (snd_pcm_recover(pcm, rc, 0) < 0) return rc;
        rc = snd_pcm_readi(pcm, buf, (snd_pcm_uframes_t)frames);
        if (rc < 0) return (rc == -EAGAIN) ? 0 : rc;
    }
    return (rc < 0) ? 0 : rc;
}
*/
import "C"

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unsafe"
)

// alsaCap wraps a raw ALSA hw capture PCM, bypassing any broken
// PulseAudio-compat layer.
type alsaCap struct {
	pcm      *C.snd_pcm_t
	isF32    bool
	channels int
	rate     int
	buf      []byte
	width    int
}

// alsaHwNames lists "hw:C,D" capture devices from /proc/asound/pcm.
func alsaHwNames() []string {
	data, err := os.ReadFile("/proc/asound/pcm")
	if err != nil {
		return nil
	}
	var names []string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, "capture") {
			continue
		}
		// "01-00: ..." → "hw:1,0" (strip zero padding, ALSA wants decimal).
		id := strings.TrimSpace(strings.SplitN(line, ":", 2)[0])
		parts := strings.Split(id, "-")
		if len(parts) != 2 {
			continue
		}
		trim := func(s string) string {
			if s = strings.TrimLeft(s, "0"); s == "" {
				return "0"
			}
			return s
		}
		names = append(names, fmt.Sprintf("hw:%s,%s", trim(parts[0]), trim(parts[1])))
	}
	return names
}

func openALSA() (*alsaCap, error) {
	var lastErr error
	for _, name := range alsaHwNames() {
		cname := C.CString(name)
		var pcm *C.snd_pcm_t
		var fmtFlag, chans C.int
		var rate C.uint
		rc := C.pmv_alsa_open(&pcm, cname, &fmtFlag, &chans, &rate)
		C.free(unsafe.Pointer(cname))
		if rc < 0 {
			lastErr = fmt.Errorf("alsa open %s: %s", name, C.GoString(C.snd_strerror(rc)))
			continue
		}
		f32 := fmtFlag != 0
		width := 2
		if f32 {
			width = 4
		}
		ch := int(chans)
		if ch <= 0 {
			ch = 1
		}
		return &alsaCap{
			pcm:      pcm,
			isF32:    f32,
			channels: ch,
			rate:     int(rate),
			buf:      make([]byte, 8192),
			width:    width,
		}, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no capture devices in /proc/asound/pcm")
	}
	return nil, lastErr
}

// probe reads until d elapses or data arrives. Reports whether bytes flowed.
func (a *alsaCap) probe(d time.Duration) (int, bool, int, int) {
	total := 0
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if n := a.readi(); n > 0 {
			total += n
			if total > 0 {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return total, a.isF32, a.channels, a.rate
}

// readi reads one chunk; returns bytes read or 0.
func (a *alsaCap) readi() int {
	frames := len(a.buf) / (a.width * a.channels)
	n := C.pmv_alsa_read(a.pcm, unsafe.Pointer(&a.buf[0]), C.int(frames))
	if n <= 0 {
		return 0
	}
	return int(n) * a.width * a.channels
}

func (a *alsaCap) read() []byte {
	n := a.readi()
	if n == 0 {
		return nil
	}
	return a.buf[:n]
}

func (a *alsaCap) Close() {
	if a.pcm != nil {
		C.snd_pcm_close(a.pcm)
		a.pcm = nil
	}
}
