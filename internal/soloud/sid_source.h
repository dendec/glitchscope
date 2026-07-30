#ifndef MDPP_SID_SOURCE_H
#define MDPP_SID_SOURCE_H

#include "soloud.h"

namespace SoLoud {
class SidSource : public AudioSource {
public:
    SidSource();
    ~SidSource() override;
    result loadMem(const unsigned char *data, unsigned int length, bool copy = true);
    AudioSourceInstance *createInstance() override;
    double getLengthSeconds() const;
    int getTrackCount() const;
    const char *getTitle() const;
    const char *getAuthor() const;

    unsigned char *mData;
    unsigned int mDataLength;
    unsigned int mLengthMs;
    char *mTitle;
    char *mAuthor;
};
}

extern "C" {
void *SidSource_create();
void SidSource_destroy(void *source);
int SidSource_loadMem(void *source, const unsigned char *data, unsigned int length);
unsigned int SidSource_getLengthMs(void *source);
unsigned int SidSource_getTrackCount(void *source);
const char *SidSource_getTitle(void *source);
const char *SidSource_getAuthor(void *source);
unsigned int SidSource_getSampleRate(void *source);
}

#endif