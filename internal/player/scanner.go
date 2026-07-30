package player

import (
	"strings"
)

// skipPrefixes lists directories to skip during music scan.
var skipPrefixes = []string{
	"/usr", "/opt", "/etc", "/var", "/tmp",
	"/sys", "/proc", "/dev", "/boot",
	"/lib", "/bin", "/sbin",
}

// SupportedExts maps file extensions the scanner looks for.
var SupportedExts = map[string]bool{
	".aac": true, ".ac3": true, ".eac3": true,
	".mp1": true, ".mp2": true, ".mp3": true,
	".ogg": true, ".oga": true, ".opus": true,
	".flac": true, ".wav": true, ".rf64": true,
	".aiff": true, ".aif": true, ".aifc": true, ".caf": true,
	".m4a": true, ".m4b": true, ".mp4": true, ".mov": true,
	".mka": true, ".mkv": true, ".webm": true,
	".wma": true, ".asf": true, ".amr": true,
	".ape": true, ".tta": true, ".ts": true, ".m2ts": true,
	".ra": true, ".rm": true,
	// Tracker formats via libxmp (core)
	".mod": true, ".xm": true, ".it": true, ".s3m": true,
	// Additional tracker formats
	".mptm": true, ".stm": true, ".nst": true, ".wow": true,
	".ult": true, ".669": true, ".mtm": true, ".med": true,
	".far": true, ".mdl": true, ".ams": true, ".dsm": true,
	".amf": true, ".okt": true, ".dmf": true, ".ptm": true,
	".psm": true, ".mt2": true, ".dbm": true,
	// Exotic formats
	".abk": true, ".digi": true, ".dtt": true,
	".flx": true, ".gtk": true, ".imf": true, ".liq": true,
	".masi": true, ".mgt": true, ".mmd": true, ".mmdc": true,
	".mmcmp": true, ".muse": true, ".nt": true, ".pmd": true,
	".ppm": true, ".pru": true, ".pt36": true, ".rh": true,
	".rtm": true, ".sfx": true, ".sfx2": true, ".stim": true,
	".stx": true, ".tcb": true, ".tdd": true, ".tp": true,
	".uni": true, ".xd": true,
	// Game Music Emu formats
	".ay": true, ".nsf": true, ".nsfe": true, ".spc": true, ".gbs": true,
	".hes": true, ".kss": true, ".sap": true,
	".vgm": true, ".vgz": true,
	".sid": true, ".rsid": true,
	// AY/YM VTX via libayumi
	".vtx": true,
	".pt3": true,
	".ym":  true, ".lh": true, ".lha": true,
	// libopenmpt fallback formats
	".mo3": true, ".ktm": true, ".ims": true, ".mdc": true,
	".spx": true, ".txn": true,
}

func shouldSkipDir(path string) bool {
	for _, p := range skipPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}
