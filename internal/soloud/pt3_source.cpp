#include "pt3_source.h"

#include <algorithm>
#include <cstring>
#include <mutex>
#include <new>
#include <stdint.h>

extern "C" {
#include "../../lib/ayumi/ayumi.h"
#include "../../lib/pt3player/pt3player.h"
}

namespace SoLoud {
namespace {
std::mutex pt3Mutex;
const unsigned int sampleRate = 44100;
const unsigned int frameRate = 50;
const unsigned int maxLengthSeconds = 180;
}

class Pt3Instance : public AudioSourceInstance {
public:
    explicit Pt3Instance(Pt3Source *parent)
        : mParent(parent), mAyumi(), mFrame(0), mSampleInFrame(0), mPlaying(false) {
        std::lock_guard<std::mutex> lock(pt3Mutex);
        if (!mParent->mData || !func_setup_music(mParent->mData, mParent->mDataLength, 1, 0) ||
            !func_restart_music(1) || !ayumi_configure(&mAyumi, 0, 1773400, sampleRate)) {
            return;
        }
        setPan();
        mPlaying = true;
    }

    unsigned int getAudio(float *buffer, unsigned int frames, unsigned int bufferSize) override {
        if (!mPlaying) {
            return 0;
        }
        unsigned int produced = 0;
        for (; produced < frames && mPlaying; ++produced) {
            if (mSampleInFrame == 0) {
                std::lock_guard<std::mutex> lock(pt3Mutex);
                func_play_tick(1);
                func_getregs(mRegisters, 1);
                applyRegisters();
            }
            ayumi_process(&mAyumi);
            buffer[produced] = static_cast<float>(mAyumi.left);
            buffer[bufferSize + produced] = static_cast<float>(mAyumi.right);
            ++mSampleInFrame;
            if (mSampleInFrame == sampleRate / frameRate) {
                mSampleInFrame = 0;
                ++mFrame;
                if (mFrame >= maxLengthSeconds * frameRate) {
                    mPlaying = false;
                }
            }
        }
        return produced;
    }

    bool hasEnded() override { return !mPlaying; }

    result seek(time seconds, float *, unsigned int) override {
        const unsigned int frame = static_cast<unsigned int>(std::max(0.0, seconds) * frameRate);
        if (frame >= maxLengthSeconds * frameRate) {
            mFrame = maxLengthSeconds * frameRate;
            mPlaying = false;
            return SO_NO_ERROR;
        }
        std::lock_guard<std::mutex> lock(pt3Mutex);
        if (!func_restart_music(1)) {
            return FILE_LOAD_FAILED;
        }
        resetAyumi();
        mFrame = 0;
        mSampleInFrame = 0;
        for (unsigned int i = 0; i < frame; ++i) {
            func_play_tick(1);
            func_getregs(mRegisters, 1);
            applyRegisters();
            for (unsigned int sample = 0; sample < sampleRate / frameRate; ++sample) {
                ayumi_process(&mAyumi);
            }
            mFrame++;
        }
        mPlaying = true;
        return SO_NO_ERROR;
    }

    result rewind() override { return seek(0, NULL, 0); }

private:
    void setPan() {
        ayumi_set_pan(&mAyumi, 0, 0.1, 0);
        ayumi_set_pan(&mAyumi, 1, 0.5, 0);
        ayumi_set_pan(&mAyumi, 2, 0.9, 0);
    }

    void resetAyumi() {
        ayumi_configure(&mAyumi, 0, 1773400, sampleRate);
        setPan();
    }

    void applyRegisters() {
        ayumi_set_tone(&mAyumi, 0, mRegisters[1] << 8 | mRegisters[0]);
        ayumi_set_tone(&mAyumi, 1, mRegisters[3] << 8 | mRegisters[2]);
        ayumi_set_tone(&mAyumi, 2, mRegisters[5] << 8 | mRegisters[4]);
        ayumi_set_noise(&mAyumi, mRegisters[6]);
        ayumi_set_mixer(&mAyumi, 0, mRegisters[7] & 1, (mRegisters[7] >> 3) & 1, mRegisters[8] >> 4);
        ayumi_set_mixer(&mAyumi, 1, (mRegisters[7] >> 1) & 1, (mRegisters[7] >> 4) & 1, mRegisters[9] >> 4);
        ayumi_set_mixer(&mAyumi, 2, (mRegisters[7] >> 2) & 1, (mRegisters[7] >> 5) & 1, mRegisters[10] >> 4);
        ayumi_set_volume(&mAyumi, 0, mRegisters[8] & 15);
        ayumi_set_volume(&mAyumi, 1, mRegisters[9] & 15);
        ayumi_set_volume(&mAyumi, 2, mRegisters[10] & 15);
        ayumi_set_envelope(&mAyumi, mRegisters[12] << 8 | mRegisters[11]);
        if (mRegisters[13] != 255) {
            ayumi_set_envelope_shape(&mAyumi, mRegisters[13]);
        }
    }

    Pt3Source *mParent;
    struct ayumi mAyumi;
    unsigned char mRegisters[14];
    unsigned int mFrame;
    unsigned int mSampleInFrame;
    bool mPlaying;
};

Pt3Source::Pt3Source() : mData(NULL), mDataLength(0) {
    mBaseSamplerate = sampleRate;
    mChannels = 2;
}

Pt3Source::~Pt3Source() { delete[] mData; }

result Pt3Source::loadMem(const unsigned char *data, unsigned int length, bool) {
    if (!data || length < 12 || length > 65536) {
        return FILE_LOAD_FAILED;
    }
    unsigned char *copy = new (std::nothrow) unsigned char[length];
    if (!copy) {
        return OUT_OF_MEMORY;
    }
    std::memcpy(copy, data, length);
    delete[] mData;
    mData = copy;
    mDataLength = length;
    return SO_NO_ERROR;
}

AudioSourceInstance *Pt3Source::createInstance() { return new Pt3Instance(this); }
double Pt3Source::getLengthSeconds() const { return maxLengthSeconds; }
}

extern "C" {
void *Pt3_create() { return new (std::nothrow) SoLoud::Pt3Source(); }
void Pt3_destroy(void *source) { delete static_cast<SoLoud::Pt3Source *>(source); }
int Pt3_loadMem(void *source, const unsigned char *data, unsigned int length) {
    return static_cast<SoLoud::Pt3Source *>(source)->loadMem(data, length) == SoLoud::SO_NO_ERROR ? 0 : 1;
}
unsigned int Pt3_getLengthMs(void *source) {
    return static_cast<unsigned int>(static_cast<SoLoud::Pt3Source *>(source)->getLengthSeconds() * 1000.0);
}
unsigned int Pt3_getSampleRate(void *) { return 44100; }
}