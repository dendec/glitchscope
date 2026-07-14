// Unity build: compiles all needed SoLoud sources as a single translation unit.
//
// Include order matters: audio sources (which include dr_* headers with
// DR_*_IMPLEMENTATION) must come before soloud_c.cpp, because soloud_c.cpp
// includes soloud_wavstream.h which forward-declares drflac/drmp3/drwav.
// When the dr_* headers are already processed, the forward declarations
// in soloud_wavstream.h are skipped (#ifndef guard), and the full struct
// definitions from the first pass are used.
#define DR_MP3_IMPLEMENTATION
#define DR_MP3_NO_STDIO
#define DR_MP3_FLOAT_OUTPUT
#define DR_WAV_IMPLEMENTATION
#define DR_WAV_NO_STDIO
#define DR_FLAC_IMPLEMENTATION
#define DR_FLAC_NO_STDIO
#define DR_FLAC_NO_CRC

// Core engine
#include "../../lib/soloud/src/core/soloud.cpp"
#include "../../lib/soloud/src/core/soloud_audiosource.cpp"
#include "../../lib/soloud/src/core/soloud_bus.cpp"
#include "../../lib/soloud/src/core/soloud_core_3d.cpp"
#include "../../lib/soloud/src/core/soloud_core_basicops.cpp"
#include "../../lib/soloud/src/core/soloud_core_faderops.cpp"
#include "../../lib/soloud/src/core/soloud_core_filterops.cpp"
#include "../../lib/soloud/src/core/soloud_core_getters.cpp"
#include "../../lib/soloud/src/core/soloud_core_setters.cpp"
#include "../../lib/soloud/src/core/soloud_core_voicegroup.cpp"
#include "../../lib/soloud/src/core/soloud_core_voiceops.cpp"
#include "../../lib/soloud/src/core/soloud_fader.cpp"
#include "../../lib/soloud/src/core/soloud_fft.cpp"
#include "../../lib/soloud/src/core/soloud_fft_lut.cpp"
#include "../../lib/soloud/src/core/soloud_file.cpp"
#include "../../lib/soloud/src/core/soloud_filter.cpp"
#include "../../lib/soloud/src/core/soloud_misc.cpp"
#include "../../lib/soloud/src/core/soloud_queue.cpp"
#include "../../lib/soloud/src/core/soloud_thread.cpp"

// Audio sources MUST come before soloud_c.cpp
// so dr_* headers are fully processed first.
#include "../../lib/soloud/src/audiosource/wav/stb_vorbis.c"
#include "../../lib/soloud/src/audiosource/wav/soloud_wav.cpp"

// SDL2 backend
#include "../../lib/soloud/src/backend/soloud_sdl2_static.cpp"


