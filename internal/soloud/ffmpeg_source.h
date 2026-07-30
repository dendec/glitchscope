#ifndef MDPP_FFMPEG_SOURCE_H
#define MDPP_FFMPEG_SOURCE_H

#include "soloud.h"

namespace SoLoud {
class FfmpegSource : public AudioSource {
public:
    FfmpegSource();
    ~FfmpegSource() override;

    result loadMem(const unsigned char *data, unsigned int length, bool copy = true);
    AudioSourceInstance *createInstance() override;
    double getLengthSeconds() const;
    int getChannels() const;
    int getSampleRate() const;

    unsigned char *mData;
    unsigned int mDataLength;
    int mSampleRate;
    int mChannels;
    long long mDurationUs;
};
}

extern "C" {
void *Ffmpeg_create();
void Ffmpeg_destroy(void *source);
int Ffmpeg_loadMem(void *source, const unsigned char *data, unsigned int length);
unsigned int Ffmpeg_getLengthMs(void *source);
unsigned int Ffmpeg_getChannels(void *source);
unsigned int Ffmpeg_getSampleRate(void *source);
}

#endif
