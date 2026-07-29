#ifndef MDPP_AYUMI_SOURCE_H
#define MDPP_AYUMI_SOURCE_H

#include "soloud.h"

namespace SoLoud {
class AyumiSource : public AudioSource {
public:
    AyumiSource();
    ~AyumiSource() override;
    result loadMem(const unsigned char *data, unsigned int length, bool copy = true);
    AudioSourceInstance *createInstance() override;
    double getLengthSeconds() const;

    unsigned char *mRegisterData;
    unsigned int mRegisterDataLength;
    unsigned int mFrames;
    unsigned int mFrameRate;
    unsigned int mChipFrequency;
    bool mIsYm;
};
}

extern "C" {
void *Ayumi_create();
void Ayumi_destroy(void *source);
int Ayumi_loadMem(void *source, const unsigned char *data, unsigned int length);
unsigned int Ayumi_getLengthMs(void *source);
unsigned int Ayumi_getSampleRate(void *source);
}

#endif