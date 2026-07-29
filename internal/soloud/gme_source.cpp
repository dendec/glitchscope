#include "gme_source.h"

#include <algorithm>
#include <cstring>
#include <new>
#include <vector>

#include "../../lib/game-music-emu/gme/gme.h"

namespace SoLoud {
class GmeInstance : public AudioSourceInstance {
public:
    explicit GmeInstance(GmeSource *parent) : mParent(parent), mEmu(NULL), mPlaying(false) {
        mEmu = NULL;
        if (!mParent->mData || gme_open_data(mParent->mData, mParent->mDataLength, &mEmu, (int)mParent->mSampleRate)) {
            mEmu = NULL;
            return;
        }
        if (gme_start_track(mEmu, mParent->mTrack)) {
            gme_delete(mEmu);
            mEmu = NULL;
            return;
        }
        gme_set_fade(mEmu, mParent->mLengthMs);
        mPlaying = true;
    }

    ~GmeInstance() override {
        if (mEmu) {
            gme_delete(mEmu);
        }
    }

    unsigned int getAudio(float *buffer, unsigned int frames, unsigned int bufferSize) override {
        if (!mEmu || !mPlaying) {
            return 0;
        }
        std::vector<short> pcm(frames * 2);
        gme_err_t error = gme_play(mEmu, (int)pcm.size(), pcm.data());
        if (error) {
            mPlaying = false;
            return 0;
        }
        for (unsigned int i = 0; i < frames; ++i) {
            buffer[i] = pcm[i * 2] / 32768.0f;
            buffer[bufferSize + i] = pcm[i * 2 + 1] / 32768.0f;
        }
        if (gme_track_ended(mEmu)) {
            mPlaying = false;
        }
        return frames;
    }

    bool hasEnded() override {
        return !mPlaying;
    }

    result seek(time seconds, float *, unsigned int) override {
        if (!mEmu) {
            return FILE_LOAD_FAILED;
        }
        int milliseconds = (int)std::max(0.0, seconds * 1000.0);
        gme_err_t error = gme_seek(mEmu, milliseconds);
        mPlaying = error == NULL;
        return error ? FILE_LOAD_FAILED : SO_NO_ERROR;
    }

    result rewind() override {
        return seek(0, NULL, 0);
    }

private:
    GmeSource *mParent;
    Music_Emu *mEmu;
    bool mPlaying;
};

GmeSource::GmeSource()
    : mData(NULL), mDataLength(0), mTrack(0), mLengthMs(150000), mSampleRate(44100),
      mTitle(NULL), mAuthor(NULL) {
    mBaseSamplerate = (float)mSampleRate;
    mChannels = 2;
}

GmeSource::~GmeSource() {
    delete[] mData;
    delete[] mTitle;
    delete[] mAuthor;
}

result GmeSource::loadMem(const unsigned char *data, unsigned int length, bool) {
    if (!data || length == 0) {
        return FILE_LOAD_FAILED;
    }
    Music_Emu *probe = NULL;
    gme_err_t error = gme_open_data(data, length, &probe, mSampleRate);
    if (error) {
        return FILE_LOAD_FAILED;
    }
    gme_info_t *info = NULL;
    if (gme_track_info(probe, &info, 0) == NULL && info) {
        mLengthMs = info->play_length > 0 ? info->play_length : 150000;
        delete[] mTitle;
        delete[] mAuthor;
        mTitle = strdup(info->song ? info->song : "");
        mAuthor = strdup(info->author ? info->author : "");
        gme_free_info(info);
    }
    gme_delete(probe);

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

AudioSourceInstance *GmeSource::createInstance() {
    return new GmeInstance(this);
}

double GmeSource::getLengthSeconds() const {
    return mLengthMs / 1000.0;
}

int GmeSource::getTrackCount() const {
    if (!mData) {
        return 0;
    }
    Music_Emu *emu = NULL;
    if (gme_open_data(mData, mDataLength, &emu, mSampleRate)) {
        return 0;
    }
    int count = gme_track_count(emu);
    gme_delete(emu);
    return count;
}

const char *GmeSource::getTitle() const { return mTitle ? mTitle : ""; }
const char *GmeSource::getAuthor() const { return mAuthor ? mAuthor : ""; }
}

extern "C" {
void *Gme_create() { return new (std::nothrow) SoLoud::GmeSource(); }
void Gme_destroy(void *source) { delete static_cast<SoLoud::GmeSource *>(source); }
int Gme_loadMem(void *source, const unsigned char *data, unsigned int length) {
    return static_cast<SoLoud::GmeSource *>(source)->loadMem(data, length) == SoLoud::SO_NO_ERROR ? 0 : 1;
}
unsigned int Gme_getLengthMs(void *source) {
    return (unsigned int)(static_cast<SoLoud::GmeSource *>(source)->getLengthSeconds() * 1000.0);
}
unsigned int Gme_getTrackCount(void *source) {
    return (unsigned int)static_cast<SoLoud::GmeSource *>(source)->getTrackCount();
}
const char *Gme_getTitle(void *source) { return static_cast<SoLoud::GmeSource *>(source)->getTitle(); }
const char *Gme_getAuthor(void *source) { return static_cast<SoLoud::GmeSource *>(source)->getAuthor(); }
unsigned int Gme_getSampleRate(void *source) { return (unsigned int)static_cast<SoLoud::GmeSource *>(source)->mSampleRate; }
}
