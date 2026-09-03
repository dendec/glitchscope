#include "ffmpeg_source.h"

#include <algorithm>
#include <cstdio>
#include <cstring>
#include <new>
#include <string>
#include <vector>

extern "C" {
#include <libavcodec/avcodec.h>
#include <libavformat/avformat.h>
#include <libavutil/channel_layout.h>
#include <libavutil/error.h>
#include <libavutil/mem.h>
#include <libswresample/swresample.h>
}

namespace {
constexpr int kOutputRate = 44100;
constexpr int kIOBufferSize = 32 * 1024;

struct InputContext {
    const unsigned char *data = nullptr;  // memory-backed input
    size_t size = 0;
    size_t position = 0;
    FILE *file = nullptr;                 // non-null when file-backed
    AVFormatContext *format = nullptr;
    AVIOContext *io = nullptr;
};

int readPacket(void *opaque, unsigned char *buffer, int bufferSize) {
    auto *input = static_cast<InputContext *>(opaque);
    if (input->file) {
        size_t count = fread(buffer, 1, static_cast<size_t>(bufferSize), input->file);
        if (count == 0) {
            return ferror(input->file) ? AVERROR(EIO) : AVERROR_EOF;
        }
        return static_cast<int>(count);
    }
    if (input->position >= input->size) {
        return AVERROR_EOF;
    }
    size_t count = std::min(input->size - input->position,
                            static_cast<size_t>(bufferSize));
    std::memcpy(buffer, input->data + input->position, count);
    input->position += count;
    return static_cast<int>(count);
}

int64_t seekPacket(void *opaque, int64_t offset, int whence) {
    auto *input = static_cast<InputContext *>(opaque);
    if (whence == AVSEEK_SIZE) {
        if (input->file) {
            long current = ftell(input->file);
            if (fseek(input->file, 0, SEEK_END) != 0) {
                return AVERROR(EINVAL);
            }
            long end = ftell(input->file);
            fseek(input->file, current, SEEK_SET);
            return end;
        }
        return static_cast<int64_t>(input->size);
    }
    if (input->file) {
        int whenceMode = SEEK_SET;
        switch (whence & ~AVSEEK_FORCE) {
        case SEEK_SET:
            whenceMode = SEEK_SET;
            break;
        case SEEK_CUR:
            whenceMode = SEEK_CUR;
            break;
        case SEEK_END:
            whenceMode = SEEK_END;
            break;
        default:
            return AVERROR(EINVAL);
        }
        if (fseek(input->file, offset, whenceMode) != 0) {
            return AVERROR(EINVAL);
        }
        return static_cast<int64_t>(ftell(input->file));
    }

    int64_t position = 0;
    switch (whence & ~AVSEEK_FORCE) {
    case SEEK_SET:
        position = offset;
        break;
    case SEEK_CUR:
        position = static_cast<int64_t>(input->position) + offset;
        break;
    case SEEK_END:
        position = static_cast<int64_t>(input->size) + offset;
        break;
    default:
        return AVERROR(EINVAL);
    }
    if (position < 0 || position > static_cast<int64_t>(input->size)) {
        return AVERROR(EINVAL);
    }
    input->position = static_cast<size_t>(position);
    return position;
}

// Frees the AVIO/format resources and, for file-backed inputs, the FILE handle.
void closeInput(InputContext *input) {
    if (!input) {
        return;
    }
    if (input->format) {
        avformat_close_input(&input->format);
    }
    if (input->io) {
        avio_context_free(&input->io);
    }
    if (input->file) {
        fclose(input->file);
        input->file = nullptr;
    }
}

bool setupIO(InputContext *input) {
    unsigned char *ioBuffer = static_cast<unsigned char *>(av_malloc(kIOBufferSize));
    if (!ioBuffer) {
        return false;
    }
    input->io = avio_alloc_context(
        ioBuffer,
        kIOBufferSize,
        0,
        input,
        readPacket,
        nullptr,
        seekPacket);
    if (!input->io) {
        av_free(ioBuffer);
        return false;
    }
    return true;
}

// Opens the input with FFmpeg's custom IO and probes the format. On failure it
// frees everything (including the FILE handle) and returns false.
bool finishOpen(InputContext *input) {
    input->format = avformat_alloc_context();
    if (!input->format) {
        closeInput(input);
        return false;
    }
    input->format->pb = input->io;
    input->format->flags |= AVFMT_FLAG_CUSTOM_IO;
    if (avformat_open_input(&input->format, nullptr, nullptr, nullptr) < 0 ||
        avformat_find_stream_info(input->format, nullptr) < 0) {
        closeInput(input);
        return false;
    }
    return true;
}

bool openMemInput(const unsigned char *data, size_t length, InputContext *input) {
    input->data = data;
    input->size = length;
    input->position = 0;
    input->file = nullptr;
    input->format = nullptr;
    input->io = nullptr;
    if (!setupIO(input)) {
        return false;
    }
    return finishOpen(input);
}

bool openFileInput(const char *path, InputContext *input) {
    input->data = nullptr;
    input->size = 0;
    input->position = 0;
    input->format = nullptr;
    input->io = nullptr;
    input->file = fopen(path, "rb");
    if (!input->file) {
        return false;
    }
    if (!setupIO(input)) {
        closeInput(input);
        return false;
    }
    return finishOpen(input);
}

int findAudioStream(AVFormatContext *format) {
    return av_find_best_stream(format, AVMEDIA_TYPE_AUDIO, -1, -1, nullptr, 0);
}

int64_t streamDurationUs(const AVFormatContext *format, int streamIndex) {
    const AVStream *stream = format->streams[streamIndex];
    if (stream->duration != AV_NOPTS_VALUE) {
        return av_rescale_q(stream->duration, stream->time_base, AV_TIME_BASE_Q);
    }
    if (format->duration != AV_NOPTS_VALUE) {
        return format->duration;
    }
    return 0;
}
}

namespace SoLoud {
class FfmpegInstance : public AudioSourceInstance {
public:
    explicit FfmpegInstance(FfmpegSource *parent) : mParent(parent) {
        bool opened = false;
        if (parent->mPath) {
            opened = openFileInput(parent->mPath, &mInput);
        } else if (parent->mData) {
            opened = openMemInput(parent->mData, parent->mDataLength, &mInput);
        }
        if (!opened) {
            return;
        }
        mFormat = mInput.format;

        mStreamIndex = findAudioStream(mFormat);
        if (mStreamIndex < 0) {
            return;
        }
        const AVCodecParameters *parameters = mFormat->streams[mStreamIndex]->codecpar;
        const AVCodec *codec = avcodec_find_decoder(parameters->codec_id);
        if (!codec) {
            return;
        }
        mCodec = avcodec_alloc_context3(codec);
        if (!mCodec || avcodec_parameters_to_context(mCodec, parameters) < 0 ||
            avcodec_open2(mCodec, codec, nullptr) < 0) {
            return;
        }
        mPacket = av_packet_alloc();
        mFrame = av_frame_alloc();
        if (!mPacket || !mFrame) {
            return;
        }

        AVChannelLayout outputLayout;
        av_channel_layout_default(&outputLayout, 2);
        if (swr_alloc_set_opts2(
                &mResampler,
                &outputLayout,
                AV_SAMPLE_FMT_FLTP,
                kOutputRate,
                &mCodec->ch_layout,
                mCodec->sample_fmt,
                mCodec->sample_rate,
                0,
                nullptr) < 0 ||
            !mResampler || swr_init(mResampler) < 0) {
            av_channel_layout_uninit(&outputLayout);
            return;
        }
        av_channel_layout_uninit(&outputLayout);
        mBaseSamplerate = static_cast<float>(kOutputRate);
        mChannels = 2;
        mReady = true;
    }

    ~FfmpegInstance() override {
        if (mResampler) swr_free(&mResampler);
        if (mFrame) av_frame_free(&mFrame);
        if (mPacket) av_packet_free(&mPacket);
        if (mCodec) avcodec_free_context(&mCodec);
        closeInput(&mInput);
    }

    unsigned int getAudio(float *buffer, unsigned int frames, unsigned int bufferSize) override {
        if (!mReady || mEnded) return 0;
        unsigned int produced = 0;
        while (produced < frames) {
            if (mPendingFrames == 0 && !decodeMore()) {
                mEnded = true;
                break;
            }
            unsigned int count = std::min(frames - produced, mPendingFrames);
            for (unsigned int i = 0; i < count; ++i) {
                buffer[produced + i] = mPendingLeft[i];
                buffer[bufferSize + produced + i] = mPendingRight[i];
            }
            mPendingLeft.erase(mPendingLeft.begin(), mPendingLeft.begin() + count);
            mPendingRight.erase(mPendingRight.begin(), mPendingRight.begin() + count);
            mPendingFrames -= count;
            produced += count;
        }
        return produced;
    }

    bool hasEnded() override { return mEnded; }

    result seek(time seconds, float *, unsigned int) override {
        if (!mReady) return FILE_LOAD_FAILED;
        int64_t timestamp = av_rescale_q(
            static_cast<int64_t>(seconds * AV_TIME_BASE),
            AV_TIME_BASE_Q,
            mFormat->streams[mStreamIndex]->time_base);
        if (avformat_seek_file(mFormat, mStreamIndex, INT64_MIN, timestamp,
                               INT64_MAX, AVSEEK_FLAG_BACKWARD) < 0) {
            return FILE_LOAD_FAILED;
        }
        avcodec_flush_buffers(mCodec);
        swr_close(mResampler);
        if (swr_init(mResampler) < 0) return FILE_LOAD_FAILED;
        mPendingLeft.clear();
        mPendingRight.clear();
        mPendingFrames = 0;
        mEnded = false;
        return SO_NO_ERROR;
    }

    result rewind() override { return seek(0, nullptr, 0); }

private:
    bool decodeMore() {
        while (true) {
            int error = av_read_frame(mFormat, mPacket);
            if (error < 0) {
                avcodec_send_packet(mCodec, nullptr);
                error = AVERROR_EOF;
            } else if (mPacket->stream_index != mStreamIndex) {
                av_packet_unref(mPacket);
                continue;
            } else {
                error = avcodec_send_packet(mCodec, mPacket);
                av_packet_unref(mPacket);
            }
            if (error < 0 && error != AVERROR_EOF) return false;

            while (true) {
                error = avcodec_receive_frame(mCodec, mFrame);
                if (error == AVERROR(EAGAIN)) break;
                if (error == AVERROR_EOF) return false;
                if (error < 0) return false;
                appendFrame();
                av_frame_unref(mFrame);
                if (mPendingFrames != 0) return true;
            }
            if (mEnded) return false;
        }
    }

    void appendFrame() {
        int outputFrames = swr_get_out_samples(mResampler, mFrame->nb_samples);
        if (outputFrames <= 0) return;
        std::vector<float> left(outputFrames);
        std::vector<float> right(outputFrames);
        uint8_t *output[] = {
            reinterpret_cast<uint8_t *>(left.data()),
            reinterpret_cast<uint8_t *>(right.data())};
        int converted = swr_convert(
            mResampler,
            output,
            outputFrames,
            const_cast<const uint8_t **>(mFrame->extended_data),
            mFrame->nb_samples);
        if (converted <= 0) return;
        mPendingLeft.insert(mPendingLeft.end(), left.begin(), left.begin() + converted);
        mPendingRight.insert(mPendingRight.end(), right.begin(), right.begin() + converted);
        mPendingFrames += static_cast<unsigned int>(converted);
    }

    FfmpegSource *mParent;
    InputContext mInput;
    AVFormatContext *mFormat = nullptr;
    AVCodecContext *mCodec = nullptr;
    AVPacket *mPacket = nullptr;
    AVFrame *mFrame = nullptr;
    SwrContext *mResampler = nullptr;
    int mStreamIndex = -1;
    bool mReady = false;
    bool mEnded = false;
    unsigned int mPendingFrames = 0;
    std::vector<float> mPendingLeft;
    std::vector<float> mPendingRight;
};

FfmpegSource::FfmpegSource()
    : mData(nullptr), mDataLength(0), mPath(nullptr), mSampleRate(kOutputRate),
      mChannels(2), mDurationUs(0) {
    mBaseSamplerate = static_cast<float>(mSampleRate);
    mChannels = 2;
}

FfmpegSource::~FfmpegSource() {
    delete[] mData;
    delete[] mPath;
}

result FfmpegSource::loadMem(const unsigned char *data, unsigned int length, bool) {
    if (!data || length == 0) return FILE_LOAD_FAILED;
    InputContext input;
    if (!openMemInput(data, length, &input)) return FILE_LOAD_FAILED;
    int streamIndex = findAudioStream(input.format);
    if (streamIndex < 0) {
        closeInput(&input);
        return FILE_LOAD_FAILED;
    }
    int64_t durationUs = streamDurationUs(input.format, streamIndex);
    unsigned char *copy = new (std::nothrow) unsigned char[length];
    if (!copy) {
        closeInput(&input);
        return OUT_OF_MEMORY;
    }
    std::memcpy(copy, data, length);
    closeInput(&input);
    delete[] mData;
    mData = copy;
    mDataLength = length;
    delete[] mPath;
    mPath = nullptr;
    mDurationUs = durationUs;
    return SO_NO_ERROR;
}

// loadFile keeps only the path; each playback instance streams the file from
// disk via custom AVIO, so the whole file is never held in memory.
result FfmpegSource::loadFile(const char *path) {
    if (!path || !*path) return INVALID_PARAMETER;
    InputContext input;
    if (!openFileInput(path, &input)) return FILE_LOAD_FAILED;
    int streamIndex = findAudioStream(input.format);
    if (streamIndex < 0) {
        closeInput(&input);
        return FILE_LOAD_FAILED;
    }
    int64_t durationUs = streamDurationUs(input.format, streamIndex);
    closeInput(&input);

    const size_t pathLen = std::strlen(path);
    char *pathCopy = new (std::nothrow) char[pathLen + 1];
    if (!pathCopy) return OUT_OF_MEMORY;
    std::memcpy(pathCopy, path, pathLen + 1);

    delete[] mData;
    mData = nullptr;
    mDataLength = 0;
    delete[] mPath;
    mPath = pathCopy;
    mDurationUs = durationUs;
    return SO_NO_ERROR;
}

AudioSourceInstance *FfmpegSource::createInstance() { return new FfmpegInstance(this); }
double FfmpegSource::getLengthSeconds() const { return mDurationUs > 0 ? mDurationUs / 1000000.0 : 0.0; }
int FfmpegSource::getChannels() const { return mChannels; }
int FfmpegSource::getSampleRate() const { return mSampleRate; }
}

extern "C" {
void *Ffmpeg_create() { return new (std::nothrow) SoLoud::FfmpegSource(); }
void Ffmpeg_destroy(void *source) { delete static_cast<SoLoud::FfmpegSource *>(source); }
int Ffmpeg_loadMem(void *source, const unsigned char *data, unsigned int length) {
    return static_cast<SoLoud::FfmpegSource *>(source)->loadMem(data, length) == SoLoud::SO_NO_ERROR ? 0 : 1;
}
int Ffmpeg_loadFile(void *source, const char *path) {
    return static_cast<SoLoud::FfmpegSource *>(source)->loadFile(path) == SoLoud::SO_NO_ERROR ? 0 : 1;
}
unsigned int Ffmpeg_getLengthMs(void *source) {
    return static_cast<unsigned int>(static_cast<SoLoud::FfmpegSource *>(source)->getLengthSeconds() * 1000.0);
}
unsigned int Ffmpeg_getChannels(void *source) {
    return static_cast<unsigned int>(static_cast<SoLoud::FfmpegSource *>(source)->getChannels());
}
unsigned int Ffmpeg_getSampleRate(void *source) {
    return static_cast<unsigned int>(static_cast<SoLoud::FfmpegSource *>(source)->getSampleRate());
}

// Reads audio metadata tags from a file. Returns a newline-separated
// list of key=value pairs. Caller must free() the returned string.
// Returns NULL on failure.
char *Ffmpeg_readTags(const char *path) {
    if (!path) return nullptr;
    // The Docker builder FFmpeg may lack the file: protocol handler, so
    // avformat_open_input(path) fails with "Protocol not found".  Work
    // around this by reading through a custom AVIO context — the same
    // approach used by FfmpegSource::loadFile / openFileInput.
    InputContext input;
    if (!openFileInput(path, &input)) {
        fprintf(stderr, "[Ffmpeg_readTags] openFileInput failed: %s\n", path);
        return nullptr;
    }
    AVFormatContext *fmt = input.format;
    // NOTE: avformat_find_stream_info was already called by finishOpen
    // inside openFileInput. Calling it again crashes on custom AVIO when
    // the stream has already been fully probed (e.g. M4A with cover art
    // that FFmpeg can't decode). Skip it — fmt->metadata is already
    // populated after the first call.

    // Collect tags into a buffer. Start with format-level metadata
    // (works for MP3/FLAC/Ogg/Opus). For M4A/MOV containers the tags
    // may live only at the stream level, so fall back to iterating
    // stream metadata when the format dict is empty or incomplete.
    std::string buf;
    const AVDictionaryEntry *e = nullptr;

    // Pass 1 — format-level metadata.
    while ((e = av_dict_iterate(fmt->metadata, e))) {
        if (!e->key || !e->value) continue;
        if (strncmp(e->key, "filename", 8) == 0) continue;
        if (strncmp(e->key, "format", 6) == 0) continue;
        if (strncmp(e->key, "stream", 6) == 0) continue;
        buf += e->key;
        buf += "=";
        buf += e->value;
        buf += "\n";
    }

    // Pass 2 — stream-level metadata (fallback for M4A/MOV).
    // Only add keys not already present from the format level.
    for (unsigned int i = 0; i < fmt->nb_streams; ++i) {
        const AVStream *stream = fmt->streams[i];
        if (!stream || !stream->metadata) continue;
        e = nullptr;
        while ((e = av_dict_iterate(stream->metadata, e))) {
            if (!e->key || !e->value) continue;
            if (strncmp(e->key, "filename", 8) == 0) continue;
            // Skip if this key already appeared in buf.
            std::string needle = std::string(e->key) + "=";
            if (buf.find(needle) != std::string::npos) {
                continue;
            }
            buf += e->key;
            buf += "=";
            buf += e->value;
            buf += "\n";
        }
    }

    closeInput(&input);  // frees format, AVIO, and FILE
    if (buf.empty()) return nullptr;
    return strdup(buf.c_str());
}

// Extracts embedded cover art (album art) from an audio file.
// Returns a newly allocated buffer containing the raw image data
// (typically JPEG). Caller must free() the returned buffer.
// out_size receives the buffer length. Returns NULL on failure.
unsigned char *Ffmpeg_readCoverArt(const char *path, unsigned int *out_size) {
    if (!path || !out_size) return nullptr;
    *out_size = 0;
    InputContext input;
    if (!openFileInput(path, &input)) {
        return nullptr;
    }
    AVFormatContext *fmt = input.format;

    // Find the attached picture stream (album art).
    int picStream = -1;
    for (unsigned int i = 0; i < fmt->nb_streams; ++i) {
        if (fmt->streams[i]->codecpar->codec_type == AVMEDIA_TYPE_VIDEO &&
            (fmt->streams[i]->disposition & AV_DISPOSITION_ATTACHED_PIC)) {
            picStream = static_cast<int>(i);
            break;
        }
    }
    if (picStream < 0) {
        closeInput(&input);
        return nullptr;
    }

    // The attached picture is stored as a single packet in
    // fmt->streams[picStream]->attached_pic. We can extract it
    // directly without decoding.
    AVPacket *pkt = &fmt->streams[picStream]->attached_pic;
    if (pkt->size <= 0) {
        closeInput(&input);
        return nullptr;
    }

    unsigned char *copy = static_cast<unsigned char *>(av_malloc(pkt->size));
    if (!copy) {
        closeInput(&input);
        return nullptr;
    }
    memcpy(copy, pkt->data, pkt->size);
    *out_size = static_cast<unsigned int>(pkt->size);
    closeInput(&input);
    return copy;
}
}
