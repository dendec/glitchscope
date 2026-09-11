#ifndef GLITCHSCOPE_STREAM_SOURCE_H
#define GLITCHSCOPE_STREAM_SOURCE_H

#include "soloud.h"

namespace SoLoud {
class FfmpegStreamSource : public AudioSource {
public:
    FfmpegStreamSource();
    ~FfmpegStreamSource() override;

    AudioSourceInstance *createInstance() override;
    int write(const unsigned char *data, unsigned int length);
    void closeInput();
    void abort();
    int status() const;
    unsigned int bufferedFrames() const;
    const char *error() const;

    int mSampleRate;
    void *mState;
};
}

extern "C" {
void *FfmpegStream_create();
void FfmpegStream_destroy(void *source);
int FfmpegStream_write(void *source, const unsigned char *data, unsigned int length);
void FfmpegStream_closeInput(void *source);
void FfmpegStream_abort(void *source);
int FfmpegStream_status(void *source);
unsigned int FfmpegStream_bufferedFrames(void *source);
const char *FfmpegStream_error(void *source);
}

#endif
