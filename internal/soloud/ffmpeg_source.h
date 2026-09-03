#ifndef MDPP_FFMPEG_SOURCE_H
#define MDPP_FFMPEG_SOURCE_H

#include "soloud.h"

namespace SoLoud {
class FfmpegSource : public AudioSource {
public:
    FfmpegSource();
    ~FfmpegSource() override;

    result loadMem(const unsigned char *data, unsigned int length, bool copy = true);
    result loadFile(const char *path);
    AudioSourceInstance *createInstance() override;
    double getLengthSeconds() const;
    int getChannels() const;
    int getSampleRate() const;

    unsigned char *mData;
    unsigned int mDataLength;
    char *mPath; // non-null when file-backed (owned by this source)
    int mSampleRate;
    int mChannels;
    long long mDurationUs;
};
}

extern "C" {
void *Ffmpeg_create();
void Ffmpeg_destroy(void *source);
int Ffmpeg_loadMem(void *source, const unsigned char *data, unsigned int length);
int Ffmpeg_loadFile(void *source, const char *path);
unsigned int Ffmpeg_getLengthMs(void *source);
unsigned int Ffmpeg_getChannels(void *source);
unsigned int Ffmpeg_getSampleRate(void *source);
char *Ffmpeg_readTags(const char *path);
unsigned char *Ffmpeg_readCoverArt(const char *path, unsigned int *out_size);
}

#endif
