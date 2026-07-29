// Minimal C bridge for SoLoud.
// Does NOT include soloud_c.h to avoid typedef ambiguity (Soloud = void* vs SoLoud::Soloud).
// Uses void* for opaque handles matching the C API convention.
#include "soloud.h"
#include "soloud_wav.h"
#include "soloud_xmp.h"
#include "soloud_openmpt.h"
#include "gme_source.h"

using namespace SoLoud;

extern "C" {

void Soloud_destroy(void * aClassPtr) { delete (Soloud *)aClassPtr; }
void * Soloud_create() { return (void *)new Soloud; }

int Soloud_initEx(void * aClassPtr, unsigned int aFlags, unsigned int aBackend, unsigned int aSamplerate, unsigned int aBufferSize, unsigned int aChannels) {
	return ((Soloud *)aClassPtr)->init(aFlags, (Soloud::BACKENDS)aBackend, aSamplerate, aBufferSize, aChannels);
}

void Soloud_deinit(void * aClassPtr) {
	((Soloud *)aClassPtr)->deinit();
}

unsigned int Soloud_play(void * aClassPtr, void * aSound) {
	return ((Soloud *)aClassPtr)->play(*(AudioSource *)aSound);
}

int Soloud_seek(void * aClassPtr, unsigned int aVoiceHandle, double aSeconds) {
	return ((Soloud *)aClassPtr)->seek(aVoiceHandle, aSeconds);
}

void Soloud_stopAll(void * aClassPtr) {
	((Soloud *)aClassPtr)->stopAll();
}

	void Soloud_setPause(void * aClassPtr, unsigned int aVoiceHandle, int aPause) {
		((Soloud *)aClassPtr)->setPause(aVoiceHandle, aPause != 0);
	}

	int Soloud_getPause(void * aClassPtr, unsigned int aVoiceHandle) {
		return ((Soloud *)aClassPtr)->getPause(aVoiceHandle) ? 1 : 0;
	}

	int Soloud_isValidVoiceHandle(void * aClassPtr, unsigned int aVoiceHandle) {
		return ((Soloud *)aClassPtr)->isValidVoiceHandle(aVoiceHandle) ? 1 : 0;
	}

	float * Soloud_calcFFT(void * aClassPtr) {
		return ((Soloud *)aClassPtr)->calcFFT();
	}

	float * Soloud_getWave(void * aClassPtr) {
		return ((Soloud *)aClassPtr)->getWave();
	}

void * Xmp_create() { return (void *)new Xmp; }

void Xmp_destroy(void * aClassPtr) { delete (Xmp *)aClassPtr; }

int Xmp_loadMemEx(void * aClassPtr, const unsigned char * aMem, unsigned int aLength, int aCopy, int aTakeOwnership) {
	return ((Xmp *)aClassPtr)->loadMem(aMem, aLength, !!aCopy, !!aTakeOwnership);
}

void * Openmpt_create() { return (void *)new Openmpt; }

void Openmpt_destroy(void * aClassPtr) { delete (Openmpt *)aClassPtr; }

int Openmpt_loadMemEx(void * aClassPtr, const unsigned char * aMem, unsigned int aLength, int aCopy, int aTakeOwnership) {
	return ((Openmpt *)aClassPtr)->loadMem(aMem, aLength, !!aCopy, !!aTakeOwnership);
}

void Wav_destroy(void * aClassPtr) { delete (Wav *)aClassPtr; }
void * Wav_create() { return (void *)new Wav; }

int Wav_load(void * aClassPtr, const char * aFilename) {
	return ((Wav *)aClassPtr)->load(aFilename);
}

double Soloud_getStreamTime(void * aClassPtr, unsigned int aVoiceHandle) {
	return ((Soloud *)aClassPtr)->getStreamTime(aVoiceHandle);
}

float Soloud_getSamplerate(void * aClassPtr, unsigned int aVoiceHandle) {
	return ((Soloud *)aClassPtr)->getSamplerate(aVoiceHandle);
}

float Soloud_getInfo(void * aClassPtr, unsigned int aVoiceHandle, unsigned int aInfoKey) {
	return ((Soloud *)aClassPtr)->getInfo(aVoiceHandle, aInfoKey);
}

double Wav_getLength(void * aClassPtr) {
	return ((Wav *)aClassPtr)->getLength();
}

unsigned int Wav_getChannels(void * aClassPtr) {
	return ((Wav *)aClassPtr)->mChannels;
}

} // extern "C"
