//go:build ignore
// +build ignore

#include "bridge_sid.h"

#include <pthread.h>
#include <stdlib.h>
#include <string.h>

#include "../../lib/cRSID/libcRSID.h"

#define SID_SAMPLE_RATE 44100
#define SID_FALLBACK_LENGTH_MS 150000
#define SID_HEADER_SIZE 0x76

struct SidCodec {
    unsigned char *data;
    unsigned int length;
    unsigned int tracks;
    char title[33];
    char author[33];
};

static pthread_mutex_t sid_mutex = PTHREAD_MUTEX_INITIALIZER;

static int sid_header_valid(const unsigned char *data, unsigned int length) {
    if (!data || length < SID_HEADER_SIZE) return 0;
    if (memcmp(data, "PSID", 4) != 0 && memcmp(data, "RSID", 4) != 0) return 0;
    unsigned int header_size = ((unsigned int)data[6] << 8) | data[7];
    unsigned int subtunes = ((unsigned int)data[14] << 8) | data[15];
    if (header_size < SID_HEADER_SIZE || header_size >= length) return 0;
    return subtunes != 0;
}

static void sid_copy_text(char *dst, const unsigned char *src) {
    unsigned int i;
    for (i = 0; i < 32 && src[i]; ++i) dst[i] = (char)src[i];
    dst[i] = '\0';
}

SidCodec *Sid_create(void) {
    return (SidCodec *)calloc(1, sizeof(SidCodec));
}

void Sid_destroy(SidCodec *codec) {
    if (!codec) return;
    free(codec->data);
    free(codec);
}

int Sid_loadMem(SidCodec *codec, const unsigned char *data, unsigned int length) {
    if (!codec || !sid_header_valid(data, length)) return 1;
    unsigned char *copy = (unsigned char *)malloc(length);
    if (!copy) return 1;
    memcpy(copy, data, length);

    pthread_mutex_lock(&sid_mutex);
    cRSID_C64instance *c64 = cRSID_init(SID_SAMPLE_RATE, 1024);
    cRSID_SIDheader *header = c64 ? cRSID_processSIDfile(c64, copy, (int)length) : NULL;
    if (header) {
        unsigned int default_track = ((unsigned int)data[16] << 8) | data[17];
        unsigned int tracks = ((unsigned int)data[14] << 8) | data[15];
        if (default_track == 0 || default_track > tracks) default_track = 1;
        cRSID_initSIDtune(c64, header, (char)default_track);
        codec->tracks = tracks;
    }
    pthread_mutex_unlock(&sid_mutex);
    if (!header) {
        free(copy);
        return 1;
    }

    free(codec->data);
    codec->data = copy;
    codec->length = length;
    sid_copy_text(codec->title, data + 0x16);
    sid_copy_text(codec->author, data + 0x36);
    return 0;
}

int Sid_read(SidCodec *codec, int frames, int16_t *buffer) {
    if (!codec || !codec->data || !buffer || frames < 0) return 0;
    pthread_mutex_lock(&sid_mutex);
    for (int i = 0; i < frames; ++i) buffer[i] = cRSID_generateSample(&cRSID_C64);
    pthread_mutex_unlock(&sid_mutex);
    return frames;
}

unsigned int Sid_getLengthMs(const SidCodec *codec) { return codec && codec->data ? SID_FALLBACK_LENGTH_MS : 0; }
unsigned int Sid_getTrackCount(const SidCodec *codec) { return codec ? codec->tracks : 0; }
unsigned int Sid_getSampleRate(const SidCodec *codec) { return codec && codec->data ? SID_SAMPLE_RATE : 0; }
const char *Sid_getTitle(const SidCodec *codec) { return codec ? codec->title : ""; }
const char *Sid_getAuthor(const SidCodec *codec) { return codec ? codec->author : ""; }