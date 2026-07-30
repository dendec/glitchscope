#include "ym_source.h"

#include <algorithm>
#include <cstring>
#include <new>
#include <vector>

extern "C" {
#include "../../lib/libstsound/StSoundLibrary.h"
}

namespace SoLoud {
namespace {
const unsigned int sampleRate = 44100;
}

class YmInstance : public AudioSourceInstance {
public:
    explicit YmInstance(YmSource *parent)
        : mParent(parent), mMusic(NULL), mMono(true), mPlaying(false), mSamples() {
        if (!mParent->mData || !mParent->mDataLength) {
            return;
        }
        mMusic = ymMusicCreate();
        if (!mMusic || !ymMusicLoadMemory(mMusic, mParent->mData, mParent->mDataLength)) {
            if (mMusic) {
                ymMusicDestroy(mMusic);
                mMusic = NULL;
            }
            return;
        }
        ymMusicSetLoopMode(mMusic, YMFALSE);
        mMono = ymMusicIsMono(mMusic) != 0;
        ymMusicPlay(mMusic);
        mPlaying = true;
    }

    ~YmInstance() override {
        if (mMusic) {
            ymMusicDestroy(mMusic);
        }
    }

    unsigned int getAudio(float *buffer, unsigned int frames, unsigned int bufferSize) override {
        if (!mPlaying || !mMusic) {
            return 0;
        }
        mSamples.resize(frames * (mMono ? 1 : 2));
        if (!ymMusicCompute(mMusic, mSamples.data(), static_cast<ymint>(frames))) {
            mPlaying = false;
            return 0;
        }
        for (unsigned int i = 0; i < frames; ++i) {
            const unsigned int offset = mMono ? i : i * 2;
            buffer[i] = static_cast<float>(mSamples[offset]) / 32768.0f;
            buffer[bufferSize + i] = static_cast<float>(mSamples[mMono ? offset : offset + 1]) / 32768.0f;
        }
        if (ymMusicIsOver(mMusic)) {
            mPlaying = false;
        }
        return frames;
    }

    bool hasEnded() override { return !mPlaying; }

    result seek(time seconds, float *, unsigned int) override {
        if (!mMusic) {
            return FILE_LOAD_FAILED;
        }
        const double clamped = std::max(0.0, seconds);
        ymMusicSeek(mMusic, static_cast<ymu32>(clamped * 1000.0));
        ymMusicPlay(mMusic);
        mPlaying = true;
        return SO_NO_ERROR;
    }

    result rewind() override { return seek(0, NULL, 0); }

private:
    YmSource *mParent;
    YMMUSIC *mMusic;
    bool mMono;
    bool mPlaying;
    std::vector<ymsample> mSamples;
};

YmSource::YmSource() : mData(NULL), mDataLength(0), mLengthMs(0) {
    mBaseSamplerate = sampleRate;
    mChannels = 2;
}

YmSource::~YmSource() { delete[] mData; }

result YmSource::loadMem(const unsigned char *data, unsigned int length, bool) {
    if (!data || length < 16) {
        return FILE_LOAD_FAILED;
    }
    YMMUSIC *music = ymMusicCreate();
    if (!music) {
        return OUT_OF_MEMORY;
    }
    const ymbool loaded = ymMusicLoadMemory(music, const_cast<unsigned char *>(data), length);
    if (!loaded) {
        ymMusicDestroy(music);
        return FILE_LOAD_FAILED;
    }
    ymMusicInfo_t info;
    std::memset(&info, 0, sizeof(info));
    ymMusicGetInfo(music, &info);
    unsigned char *copy = new (std::nothrow) unsigned char[length];
    if (!copy) {
        ymMusicDestroy(music);
        return OUT_OF_MEMORY;
    }
    std::memcpy(copy, data, length);
    ymMusicDestroy(music);
    delete[] mData;
    mData = copy;
    mDataLength = length;
    mLengthMs = info.musicTimeInMs > 0 ? static_cast<unsigned int>(info.musicTimeInMs) : 0;
    return SO_NO_ERROR;
}

AudioSourceInstance *YmSource::createInstance() { return new YmInstance(this); }
double YmSource::getLengthSeconds() const { return static_cast<double>(mLengthMs) / 1000.0; }
}

extern "C" {
void *Ym_create() { return new (std::nothrow) SoLoud::YmSource(); }
void Ym_destroy(void *source) { delete static_cast<SoLoud::YmSource *>(source); }
int Ym_loadMem(void *source, const unsigned char *data, unsigned int length) {
    return static_cast<SoLoud::YmSource *>(source)->loadMem(data, length) == SoLoud::SO_NO_ERROR ? 0 : 1;
}
unsigned int Ym_getLengthMs(void *source) {
    return static_cast<unsigned int>(static_cast<SoLoud::YmSource *>(source)->getLengthSeconds() * 1000.0);
}
unsigned int Ym_getSampleRate(void *) { return 44100; }
}