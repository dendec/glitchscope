#include "ayumi_source.h"

#include <algorithm>
#include <cstring>
#include <new>

extern "C" {
#include "../../lib/ayumi/ayumi.h"
}

namespace SoLoud {
class AyumiInstance : public AudioSourceInstance {
public:
    explicit AyumiInstance(AyumiSource *parent)
        : mParent(parent), mAyumi(), mFrame(0), mSampleInFrame(0), mPlaying(false) {
        if (!mParent->mRegisterData || !mParent->mFrames ||
            !ayumi_configure(&mAyumi, mParent->mIsYm, mParent->mChipFrequency, 44100)) {
            return;
        }
        ayumi_set_pan(&mAyumi, 0, 0.1, 0);
        ayumi_set_pan(&mAyumi, 1, 0.5, 0);
        ayumi_set_pan(&mAyumi, 2, 0.9, 0);
        mPlaying = true;
    }

    unsigned int getAudio(float *buffer, unsigned int frames, unsigned int bufferSize) override {
        if (!mPlaying) {
            return 0;
        }
        for (unsigned int i = 0; i < frames; ++i) {
            updateState();
            ayumi_process(&mAyumi);
            buffer[i] = static_cast<float>(mAyumi.left);
            buffer[bufferSize + i] = static_cast<float>(mAyumi.right);
            ++mSampleInFrame;
            if (mSampleInFrame >= 44100 / mParent->mFrameRate) {
                mSampleInFrame = 0;
                ++mFrame;
                if (mFrame >= mParent->mFrames) {
                    mPlaying = false;
                }
            }
        }
        return frames;
    }

    bool hasEnded() override { return !mPlaying; }

    result seek(time seconds, float *, unsigned int) override {
        const unsigned int frame = static_cast<unsigned int>(std::max(0.0, seconds) * mParent->mFrameRate);
        if (frame >= mParent->mFrames) {
            mFrame = mParent->mFrames;
            mPlaying = false;
            return SO_NO_ERROR;
        }
        reset();
        for (unsigned int i = 0; i < frame * (44100 / mParent->mFrameRate); ++i) {
            updateState();
            ayumi_process(&mAyumi);
            ++mSampleInFrame;
            if (mSampleInFrame >= 44100 / mParent->mFrameRate) {
                mSampleInFrame = 0;
                ++mFrame;
            }
        }
        mFrame = frame;
        mSampleInFrame = 0;
        mPlaying = true;
        return SO_NO_ERROR;
    }

    result rewind() override { return seek(0, NULL, 0); }

private:
    void reset() {
        ayumi_configure(&mAyumi, mParent->mIsYm, mParent->mChipFrequency, 44100);
        ayumi_set_pan(&mAyumi, 0, 0.1, 0);
        ayumi_set_pan(&mAyumi, 1, 0.5, 0);
        ayumi_set_pan(&mAyumi, 2, 0.9, 0);
    }

    void updateState() {
        if (mFrame >= mParent->mFrames) {
            return;
        }
        const unsigned char *r = mParent->mRegisterData + mFrame;
        const unsigned int frames = mParent->mFrames;
        ayumi_set_tone(&mAyumi, 0, r[frames] << 8 | r[0]);
        ayumi_set_tone(&mAyumi, 1, r[3 * frames] << 8 | r[2 * frames]);
        ayumi_set_tone(&mAyumi, 2, r[5 * frames] << 8 | r[4 * frames]);
        ayumi_set_noise(&mAyumi, r[6 * frames]);
        ayumi_set_mixer(&mAyumi, 0, r[7 * frames] & 1, (r[7 * frames] >> 3) & 1, r[8 * frames] >> 4);
        ayumi_set_mixer(&mAyumi, 1, (r[7 * frames] >> 1) & 1, (r[7 * frames] >> 4) & 1, r[9 * frames] >> 4);
        ayumi_set_mixer(&mAyumi, 2, (r[7 * frames] >> 2) & 1, (r[7 * frames] >> 5) & 1, r[10 * frames] >> 4);
        ayumi_set_volume(&mAyumi, 0, r[8 * frames] & 15);
        ayumi_set_volume(&mAyumi, 1, r[9 * frames] & 15);
        ayumi_set_volume(&mAyumi, 2, r[10 * frames] & 15);
        ayumi_set_envelope(&mAyumi, r[12 * frames] << 8 | r[11 * frames]);
        if (r[13 * frames] != 255) {
            ayumi_set_envelope_shape(&mAyumi, r[13 * frames]);
        }
    }

    AyumiSource *mParent;
    struct ayumi mAyumi;
    unsigned int mFrame;
    unsigned int mSampleInFrame;
    bool mPlaying;
};

AyumiSource::AyumiSource()
    : mRegisterData(NULL), mRegisterDataLength(0), mFrames(0), mFrameRate(50),
      mChipFrequency(1773400), mIsYm(false) {
    mBaseSamplerate = 44100;
    mChannels = 2;
}

AyumiSource::~AyumiSource() { delete[] mRegisterData; }

result AyumiSource::loadMem(const unsigned char *data, unsigned int length, bool) {
    if (!data || length < 20 || (data[0] != 'a' && data[0] != 'y') || data[1] != 'y') {
        return FILE_LOAD_FAILED;
    }
    unsigned int pos = 2;
    const unsigned char layout = data[pos++];
    (void)layout;
    const unsigned int loop = data[pos] | data[pos + 1] << 8;
    (void)loop;
    pos += 2;
    mChipFrequency = data[pos] | data[pos + 1] << 8 | data[pos + 2] << 16 | data[pos + 3] << 24;
    pos += 4;
    mFrameRate = data[pos++];
    pos += 2;
    const unsigned int registerLength = data[pos] | data[pos + 1] << 8 | data[pos + 2] << 16 | data[pos + 3] << 24;
    pos += 4;
    if (mFrameRate == 0 || mFrameRate > 100 || registerLength == 0 || registerLength % 14 || registerLength > length - pos) {
        return FILE_LOAD_FAILED;
    }
    for (int i = 0; i < 5; ++i) {
        while (pos < length && data[pos++] != 0) {}
    }
    if (pos > length || registerLength != length - pos) {
        return FILE_LOAD_FAILED;
    }
    unsigned char *copy = new (std::nothrow) unsigned char[registerLength];
    if (!copy) {
        return OUT_OF_MEMORY;
    }
    std::memcpy(copy, data + pos, registerLength);
    delete[] mRegisterData;
    mRegisterData = copy;
    mRegisterDataLength = registerLength;
    mFrames = registerLength / 14;
    mIsYm = data[0] == 'y';
    return SO_NO_ERROR;
}

AudioSourceInstance *AyumiSource::createInstance() { return new AyumiInstance(this); }
double AyumiSource::getLengthSeconds() const { return mFrames ? static_cast<double>(mFrames) / mFrameRate : 0; }
}

extern "C" {
void *Ayumi_create() { return new (std::nothrow) SoLoud::AyumiSource(); }
void Ayumi_destroy(void *source) { delete static_cast<SoLoud::AyumiSource *>(source); }
int Ayumi_loadMem(void *source, const unsigned char *data, unsigned int length) {
    return static_cast<SoLoud::AyumiSource *>(source)->loadMem(data, length) == SoLoud::SO_NO_ERROR ? 0 : 1;
}
unsigned int Ayumi_getLengthMs(void *source) {
    return static_cast<unsigned int>(static_cast<SoLoud::AyumiSource *>(source)->getLengthSeconds() * 1000.0);
}
unsigned int Ayumi_getSampleRate(void *) { return 44100; }
}