#ifndef MDPP_BRIDGE_SID_H
#define MDPP_BRIDGE_SID_H

#include <stdint.h>

typedef struct SidCodec SidCodec;

#ifdef __cplusplus
extern "C" {
#endif

SidCodec *Sid_create(void);
void Sid_destroy(SidCodec *codec);
int Sid_loadMem(SidCodec *codec, const unsigned char *data, unsigned int length);
int Sid_read(SidCodec *codec, int frames, int16_t *buffer);
unsigned int Sid_getLengthMs(const SidCodec *codec);
unsigned int Sid_getTrackCount(const SidCodec *codec);
unsigned int Sid_getSampleRate(const SidCodec *codec);
const char *Sid_getTitle(const SidCodec *codec);
const char *Sid_getAuthor(const SidCodec *codec);

#ifdef __cplusplus
}
#endif

#endif