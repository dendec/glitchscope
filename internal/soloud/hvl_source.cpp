#include "hvl_source.h"

#include <algorithm>
#include <cstring>
#include <new>

extern "C" {
#ifdef MAX_CHANNELS
#undef MAX_CHANNELS
#endif
#include "../../lib/hvl/hvl_replay.h"
}

namespace SoLoud {
namespace {
const unsigned int sampleRate = 48000;
const unsigned int frameRate = 50;
const unsigned int maxLengthSeconds = 7200;
const unsigned int stereoMode = 1;
}

class HvlInstance : public AudioSourceInstance {
public:
    explicit HvlInstance(HvlSource *parent)
        : mParent(parent), mTune(NULL), mFrameBuffer(), mFramesLeft(0), mPlaying(false) {
        hvl_InitReplayer();
        mTune = hvl_LoadTune(mParent->mData, mParent->mDataLength, sampleRate, stereoMode);
        if (!mTune || !hvl_InitSubsong(mTune, 0)) {
            if (mTune) {
                hvl_FreeTune(mTune);
                mTune = NULL;
            }
            return;
        }
        mPlaying = true;
    }

    ~HvlInstance() override {
        if (mTune) {
            hvl_FreeTune(mTune);
        }
    }

    unsigned int getAudio(float *buffer, unsigned int frames, unsigned int bufferSize) override {
        if (!mPlaying) {
            return 0;
        }
        unsigned int produced = 0;
        while (produced < frames && mPlaying) {
            if (mFramesLeft == 0) {
                hvl_DecodeFrame(mTune, reinterpret_cast<int8_t *>(mFrameBuffer.left),
                                reinterpret_cast<int8_t *>(mFrameBuffer.right), 8);
                mFramesLeft = sampleRate / frameRate;
                if (mTune->ht_SongEndReached) {
                    mPlaying = false;
                }
            }
            const unsigned int count = std::min(frames - produced, mFramesLeft);
            for (unsigned int i = 0; i < count; ++i) {
                buffer[produced + i] = static_cast<float>(mFrameBuffer.left[i * 2]) / 8388608.0f;
                buffer[bufferSize + produced + i] = static_cast<float>(mFrameBuffer.right[i * 2]) / 8388608.0f;
            }
            produced += count;
            mFramesLeft -= count;
        }
        return produced;
    }

    bool hasEnded() override { return !mPlaying; }

    result seek(time seconds, float *, unsigned int) override {
        const unsigned int target = static_cast<unsigned int>(std::max(0.0, seconds) * frameRate);
        if (target >= maxLengthSeconds * frameRate) {
            mPlaying = false;
            return SO_NO_ERROR;
        }
        if (!hvl_InitSubsong(mTune, 0)) {
            return FILE_LOAD_FAILED;
        }
        mFramesLeft = 0;
        mPlaying = true;
        for (unsigned int i = 0; i < target && !mTune->ht_SongEndReached; ++i) {
            hvl_DecodeFrame(mTune, reinterpret_cast<int8_t *>(mFrameBuffer.left),
                            reinterpret_cast<int8_t *>(mFrameBuffer.right), 8);
        }
        return SO_NO_ERROR;
    }

    result rewind() override { return seek(0, NULL, 0); }

private:
    struct FrameBuffer {
        // libhvl writes one 32-bit sample every `bufmod` bytes.
        int32_t left[(sampleRate / frameRate) * 2];
        int32_t right[(sampleRate / frameRate) * 2];
    };

    HvlSource *mParent;
    hvl_tune *mTune;
    FrameBuffer mFrameBuffer;
    unsigned int mFramesLeft;
    bool mPlaying;
};

HvlSource::HvlSource() : mData(NULL), mDataLength(0), mLengthSeconds(0), mTrackCount(0) {
    mBaseSamplerate = sampleRate;
    mChannels = 2;
}

HvlSource::~HvlSource() { delete[] mData; }

result HvlSource::loadMem(const unsigned char *data, unsigned int length, bool) {
    if (!data || length < 4) {
        return FILE_LOAD_FAILED;
    }
    const bool isAhx = data[0] == 'T' && data[1] == 'H' && data[2] == 'X' && data[3] < 3;
    const bool isHvl = data[0] == 'H' && data[1] == 'V' && data[2] == 'L' && data[3] <= 1;
    if (!isAhx && !isHvl) {
        return FILE_LOAD_FAILED;
    }
    unsigned char *copy = new (std::nothrow) unsigned char[length];
    if (!copy) {
        return OUT_OF_MEMORY;
    }
    std::memcpy(copy, data, length);
    hvl_InitReplayer();
    hvl_tune *tune = hvl_LoadTune(copy, length, sampleRate, stereoMode);
    if (!tune) {
        delete[] copy;
        return FILE_LOAD_FAILED;
    }
    unsigned int frames = 0;
    while (!tune->ht_SongEndReached && frames < maxLengthSeconds * frameRate) {
        hvl_play_irq(tune);
        ++frames;
    }
    const unsigned int trackCount = tune->ht_SubsongNr + 1;
    hvl_FreeTune(tune);
    delete[] mData;
    mData = copy;
    mDataLength = length;
    mLengthSeconds = frames > 0 ? static_cast<double>(frames) / frameRate : 0;
    mTrackCount = trackCount;
    return SO_NO_ERROR;
}

AudioSourceInstance *HvlSource::createInstance() { return new (std::nothrow) HvlInstance(this); }
double HvlSource::getLengthSeconds() const { return mLengthSeconds; }
}

extern "C" {
void *Hvl_create() { return new (std::nothrow) SoLoud::HvlSource(); }
void Hvl_destroy(void *source) { delete static_cast<SoLoud::HvlSource *>(source); }
int Hvl_loadMem(void *source, const unsigned char *data, unsigned int length) {
    return static_cast<SoLoud::HvlSource *>(source)->loadMem(data, length) == SoLoud::SO_NO_ERROR ? 0 : 1;
}
unsigned int Hvl_getLengthMs(void *source) {
    return static_cast<unsigned int>(static_cast<SoLoud::HvlSource *>(source)->getLengthSeconds() * 1000.0);
}
unsigned int Hvl_getSampleRate(void *) { return 48000; }
unsigned int Hvl_getTrackCount(void *source) {
    return static_cast<SoLoud::HvlSource *>(source)->mTrackCount;
}
}