#include "sid_source.h"

#include <cstring>
#include <new>
#include <vector>

#include "bridge_sid.h"

extern "C" {
#include "bridge_sid.c"
}

namespace SoLoud {
class SidInstance : public AudioSourceInstance {
public:
    explicit SidInstance(SidSource *parent) : mParent(parent), mCodec(Sid_create()), mPlaying(false) {
        if (!mCodec || !mParent->mData || Sid_loadMem(mCodec, mParent->mData, mParent->mDataLength)) {
            if (mCodec) Sid_destroy(mCodec);
            mCodec = NULL;
            return;
        }
        mPlaying = true;
    }

    ~SidInstance() override { if (mCodec) Sid_destroy(mCodec); }

    unsigned int getAudio(float *buffer, unsigned int frames, unsigned int) override {
        if (!mCodec || !mPlaying) return 0;
        std::vector<short> pcm(frames);
        if (Sid_read(mCodec, (int)frames, pcm.data()) != (int)frames) {
            mPlaying = false;
            return 0;
        }
        for (unsigned int i = 0; i < frames; ++i) buffer[i] = pcm[i] / 32768.0f;
        return frames;
    }

    bool hasEnded() override { return !mPlaying; }
    result seek(time, float *, unsigned int) override { return NOT_IMPLEMENTED; }
    result rewind() override { return NOT_IMPLEMENTED; }

private:
    SidSource *mParent;
    SidCodec *mCodec;
    bool mPlaying;
};

SidSource::SidSource()
    : mData(NULL), mDataLength(0), mLengthMs(150000), mTitle(NULL), mAuthor(NULL) {
    mBaseSamplerate = 44100;
    mChannels = 1;
}

SidSource::~SidSource() {
    delete[] mData;
    delete[] mTitle;
    delete[] mAuthor;
}

result SidSource::loadMem(const unsigned char *data, unsigned int length, bool) {
    if (!data || !length) return FILE_LOAD_FAILED;
    SidCodec *probe = Sid_create();
    if (!probe || Sid_loadMem(probe, data, length)) {
        if (probe) Sid_destroy(probe);
        return FILE_LOAD_FAILED;
    }
    mLengthMs = Sid_getLengthMs(probe);
    delete[] mTitle;
    delete[] mAuthor;
    mTitle = strdup(Sid_getTitle(probe));
    mAuthor = strdup(Sid_getAuthor(probe));
    Sid_destroy(probe);

    unsigned char *copy = new (std::nothrow) unsigned char[length];
    if (!copy) return OUT_OF_MEMORY;
    std::memcpy(copy, data, length);
    delete[] mData;
    mData = copy;
    mDataLength = length;
    return SO_NO_ERROR;
}

AudioSourceInstance *SidSource::createInstance() { return new SidInstance(this); }
double SidSource::getLengthSeconds() const { return mLengthMs / 1000.0; }
int SidSource::getTrackCount() const {
    if (!mData) return 0;
    SidCodec *probe = Sid_create();
    if (!probe || Sid_loadMem(probe, mData, mDataLength)) {
        if (probe) Sid_destroy(probe);
        return 0;
    }
    int count = (int)Sid_getTrackCount(probe);
    Sid_destroy(probe);
    return count;
}
const char *SidSource::getTitle() const { return mTitle ? mTitle : ""; }
const char *SidSource::getAuthor() const { return mAuthor ? mAuthor : ""; }
}

extern "C" {
void *SidSource_create() { return new (std::nothrow) SoLoud::SidSource(); }
void SidSource_destroy(void *source) { delete static_cast<SoLoud::SidSource *>(source); }
int SidSource_loadMem(void *source, const unsigned char *data, unsigned int length) {
    return static_cast<SoLoud::SidSource *>(source)->loadMem(data, length) == SoLoud::SO_NO_ERROR ? 0 : 1;
}
unsigned int SidSource_getLengthMs(void *source) { return (unsigned int)(static_cast<SoLoud::SidSource *>(source)->getLengthSeconds() * 1000.0); }
unsigned int SidSource_getTrackCount(void *source) { return (unsigned int)static_cast<SoLoud::SidSource *>(source)->getTrackCount(); }
const char *SidSource_getTitle(void *source) { return static_cast<SoLoud::SidSource *>(source)->getTitle(); }
const char *SidSource_getAuthor(void *source) { return static_cast<SoLoud::SidSource *>(source)->getAuthor(); }
unsigned int SidSource_getSampleRate(void *source) { return 44100; }

// Sid_render decodes up to maxFrames frames of a SID tune into mono float32
// samples. The SID binding does not implement seek or rewind at all, so
// rendering a bounded buffer lets the player seek exactly in both directions.
// Stops at end-of-stream or when maxFrames is reached.
// Returns the number of frames actually rendered.
unsigned int Sid_render(const unsigned char *data, unsigned int length,
                        float *out, unsigned int maxFrames) {
    if (!data || !length || !out || maxFrames == 0) return 0;
    SidCodec *codec = Sid_create();
    if (!codec || Sid_loadMem(codec, data, length)) {
        if (codec) Sid_destroy(codec);
        return 0;
    }
    std::vector<short> tmp(8192);
    unsigned int rendered = 0;
    while (rendered < maxFrames) {
        unsigned int n = maxFrames - rendered;
        if (n > 8192) n = 8192;
        const int got = Sid_read(codec, (int)n, tmp.data());
        if (got <= 0) break;
        for (int i = 0; i < got; ++i) {
            out[rendered + i] = static_cast<float>(tmp[i]) / 32768.0f;
        }
        rendered += static_cast<unsigned int>(got);
        if (got < static_cast<int>(n)) break; // end of stream reached
    }
    Sid_destroy(codec);
    return rendered;
}
}