#ifndef PMV_HVL_SOURCE_H
#define PMV_HVL_SOURCE_H

#include "soloud.h"

namespace SoLoud {
class HvlSource : public AudioSource {
public:
    HvlSource();
    ~HvlSource() override;
    result loadMem(const unsigned char *data, unsigned int length, bool copy = true);
    AudioSourceInstance *createInstance() override;
    double getLengthSeconds() const;

    unsigned char *mData;
    unsigned int mDataLength;
    double mLengthSeconds;
    unsigned int mTrackCount;
};
}

extern "C" {
void *Hvl_create();
void Hvl_destroy(void *source);
int Hvl_loadMem(void *source, const unsigned char *data, unsigned int length);
unsigned int Hvl_getLengthMs(void *source);
unsigned int Hvl_getSampleRate(void *source);
unsigned int Hvl_getTrackCount(void *source);
}

#endif