#ifndef MDPP_PT3_SOURCE_H
#define MDPP_PT3_SOURCE_H

#include "soloud.h"

namespace SoLoud {
class Pt3Source : public AudioSource {
public:
    Pt3Source();
    ~Pt3Source() override;
    result loadMem(const unsigned char *data, unsigned int length, bool copy = true);
    AudioSourceInstance *createInstance() override;
    double getLengthSeconds() const;

    unsigned char *mData;
    unsigned int mDataLength;
};
}

extern "C" {
void *Pt3_create();
void Pt3_destroy(void *source);
int Pt3_loadMem(void *source, const unsigned char *data, unsigned int length);
unsigned int Pt3_getLengthMs(void *source);
unsigned int Pt3_getSampleRate(void *source);
}

#endif