#ifndef MDPP_GME_SOURCE_H
#define MDPP_GME_SOURCE_H

#include "soloud.h"

namespace SoLoud {
class GmeSource : public AudioSource {
public:
    GmeSource();
    ~GmeSource() override;
    result loadMem(const unsigned char *data, unsigned int length, bool copy = true);
    AudioSourceInstance *createInstance() override;
    double getLengthSeconds() const;
    int getTrackCount() const;
    const char *getTitle() const;
    const char *getAuthor() const;

    unsigned char *mData;
    unsigned int mDataLength;
    int mTrack;
    int mLengthMs;
    int mSampleRate;
    char *mTitle;
    char *mAuthor;
};
}

extern "C" {
void *Gme_create();
void Gme_destroy(void *source);
int Gme_loadMem(void *source, const unsigned char *data, unsigned int length);
unsigned int Gme_getLengthMs(void *source);
unsigned int Gme_getTrackCount(void *source);
const char *Gme_getTitle(void *source);
const char *Gme_getAuthor(void *source);
unsigned int Gme_getSampleRate(void *source);
}

#endif
