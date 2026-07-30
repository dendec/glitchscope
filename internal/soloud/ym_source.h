#ifndef MDPP_YM_SOURCE_H
#define MDPP_YM_SOURCE_H

#include "soloud.h"

namespace SoLoud {
class YmSource : public AudioSource {
public:
    YmSource();
    ~YmSource() override;
    result loadMem(const unsigned char *data, unsigned int length, bool copy = true);
    AudioSourceInstance *createInstance() override;
    double getLengthSeconds() const;

    unsigned char *mData;
    unsigned int mDataLength;
    unsigned int mLengthMs;
};
}

extern "C" {
void *Ym_create();
void Ym_destroy(void *source);
int Ym_loadMem(void *source, const unsigned char *data, unsigned int length);
unsigned int Ym_getLengthMs(void *source);
unsigned int Ym_getSampleRate(void *source);
}

#endif