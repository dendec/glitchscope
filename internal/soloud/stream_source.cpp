#include "stream_source.h"

#include <algorithm>
#include <atomic>
#include <cerrno>
#include <chrono>
#include <condition_variable>
#include <cstring>
#include <deque>
#include <mutex>
#include <new>
#include <string>
#include <thread>
#include <vector>

extern "C" {
#include <libavcodec/avcodec.h>
#include <libavformat/avformat.h>
#include <libavutil/channel_layout.h>
#include <libswresample/swresample.h>
}

namespace {
constexpr int kOutputRate = 44100;
constexpr int kIOBufferSize = 32 * 1024;
constexpr size_t kCompressedCapacity = 2 * 1024 * 1024;
constexpr size_t kPcmCapacity = kOutputRate * 4;

struct StreamState {
    mutable std::mutex inputMutex;
    std::condition_variable inputChanged;
    std::deque<unsigned char> compressed;
    bool inputClosed = false;
    bool aborted = false;

    mutable std::mutex pcmMutex;
    std::condition_variable pcmChanged;
    std::vector<float> left = std::vector<float>(kPcmCapacity);
    std::vector<float> right = std::vector<float>(kPcmCapacity);
    std::atomic<uint64_t> readFrame{0};
    std::atomic<uint64_t> writeFrame{0};

    mutable std::mutex stateMutex;
    std::condition_variable stateChanged;
    std::atomic<int> status{0}; // 0 = starting, 1 = ready, 2 = ended, -1 = failed
    std::string error;
    std::thread decoder;
};

struct InputContext {
    StreamState *state = nullptr;
    AVFormatContext *format = nullptr;
    AVIOContext *io = nullptr;
};

bool isAborted(StreamState *state) {
    std::lock_guard<std::mutex> lock(state->inputMutex);
    return state->aborted;
}

int readPacket(void *opaque, unsigned char *buffer, int bufferSize) {
    auto *state = static_cast<StreamState *>(opaque);
    std::unique_lock<std::mutex> lock(state->inputMutex);
    state->inputChanged.wait(lock, [state] {
        return !state->compressed.empty() || state->inputClosed || state->aborted;
    });
    if (state->aborted || state->compressed.empty()) {
        return AVERROR_EOF;
    }
    const size_t count = std::min(state->compressed.size(), static_cast<size_t>(bufferSize));
    for (size_t i = 0; i < count; ++i) {
        buffer[i] = state->compressed.front();
        state->compressed.pop_front();
    }
    lock.unlock();
    state->inputChanged.notify_all();
    return static_cast<int>(count);
}

int64_t seekPacket(void *, int64_t, int) { return AVERROR(ENOSYS); }

void closeInput(InputContext *input) {
    if (!input) return;
    if (input->format) avformat_close_input(&input->format);
    if (input->io) {
        av_freep(&input->io->buffer);
        avio_context_free(&input->io);
    }
}

bool setupInput(InputContext *input) {
    unsigned char *buffer = static_cast<unsigned char *>(av_malloc(kIOBufferSize));
    if (!buffer) return false;
    input->io = avio_alloc_context(buffer, kIOBufferSize, 0, input->state,
                                   readPacket, nullptr, seekPacket);
    if (!input->io) {
        av_free(buffer);
        return false;
    }
    input->format = avformat_alloc_context();
    if (!input->format) return false;
    input->io->seekable = 0;
    input->format->interrupt_callback.callback = [](void *opaque) {
        return isAborted(static_cast<StreamState *>(opaque)) ? 1 : 0;
    };
    input->format->interrupt_callback.opaque = input->state;
    input->format->pb = input->io;
    input->format->flags |= AVFMT_FLAG_CUSTOM_IO;
    return avformat_open_input(&input->format, nullptr, nullptr, nullptr) >= 0 &&
           avformat_find_stream_info(input->format, nullptr) >= 0;
}

void setState(StreamState *state, int status, const char *error = nullptr) {
    {
        std::lock_guard<std::mutex> lock(state->stateMutex);
        state->status = status;
        state->error = error ? error : "";
    }
    state->stateChanged.notify_all();
}

bool appendPcm(StreamState *state, const float *left, const float *right, int count) {
    // Single producer / single consumer ring: the audio callback neither locks
    // nor allocates. Release/acquire publishes samples before their indices.
    int offset = 0;
    while (offset < count && !isAborted(state)) {
        const uint64_t write = state->writeFrame.load(std::memory_order_relaxed);
        const uint64_t read = state->readFrame.load(std::memory_order_acquire);
        const size_t available = kPcmCapacity - static_cast<size_t>(write - read);
        if (!available) {
            std::unique_lock<std::mutex> lock(state->pcmMutex);
            state->pcmChanged.wait_for(lock, std::chrono::milliseconds(10));
            continue;
        }
        const size_t n = std::min(available, static_cast<size_t>(count - offset));
        for (size_t i = 0; i < n; ++i) {
            state->left[(write + i) % kPcmCapacity] = left[offset + i];
            state->right[(write + i) % kPcmCapacity] = right[offset + i];
        }
        state->writeFrame.store(write + n, std::memory_order_release);
        offset += static_cast<int>(n);
    }
    return offset == count;
}

void decodeLoop(StreamState *state) {
    InputContext input;
    input.state = state;
    if (!setupInput(&input)) {
        closeInput(&input);
        if (!isAborted(state)) setState(state, -1, "FFmpeg stream input failed");
        else setState(state, 2);
        return;
    }

    const int streamIndex = av_find_best_stream(input.format, AVMEDIA_TYPE_AUDIO, -1, -1, nullptr, 0);
    if (streamIndex < 0) {
        closeInput(&input);
        setState(state, -1, "FFmpeg stream has no audio stream");
        return;
    }
    const AVCodecParameters *parameters = input.format->streams[streamIndex]->codecpar;
    const AVCodec *codec = avcodec_find_decoder(parameters->codec_id);
    AVCodecContext *codecContext = codec ? avcodec_alloc_context3(codec) : nullptr;
    AVPacket *packet = av_packet_alloc();
    AVFrame *frame = av_frame_alloc();
    SwrContext *resampler = nullptr;
    AVChannelLayout outputLayout;
    av_channel_layout_default(&outputLayout, 2);
    bool ready = codecContext && packet && frame &&
                 avcodec_parameters_to_context(codecContext, parameters) >= 0 &&
                 avcodec_open2(codecContext, codec, nullptr) >= 0 &&
                 swr_alloc_set_opts2(&resampler, &outputLayout, AV_SAMPLE_FMT_FLTP,
                                     kOutputRate, &codecContext->ch_layout,
                                     codecContext->sample_fmt, codecContext->sample_rate,
                                     0, nullptr) >= 0 && resampler && swr_init(resampler) >= 0;
    av_channel_layout_uninit(&outputLayout);
    if (!ready) {
        if (resampler) swr_free(&resampler);
        if (frame) av_frame_free(&frame);
        if (packet) av_packet_free(&packet);
        if (codecContext) avcodec_free_context(&codecContext);
        closeInput(&input);
        setState(state, -1, "FFmpeg stream decoder initialization failed");
        return;
    }

    setState(state, 1);
    bool ended = false;
    while (!ended && !isAborted(state)) {
        int result = av_read_frame(input.format, packet);
        if (result < 0) {
            avcodec_send_packet(codecContext, nullptr);
            ended = true;
        } else if (packet->stream_index != streamIndex) {
            av_packet_unref(packet);
            continue;
        } else {
            result = avcodec_send_packet(codecContext, packet);
            av_packet_unref(packet);
            if (result < 0) break;
        }

        while (!isAborted(state)) {
            result = avcodec_receive_frame(codecContext, frame);
            if (result == AVERROR(EAGAIN) || result == AVERROR_EOF) break;
            if (result < 0) {
                ended = true;
                break;
            }
            const int outputFrames = swr_get_out_samples(resampler, frame->nb_samples);
            if (outputFrames > 0) {
                std::vector<float> left(outputFrames);
                std::vector<float> right(outputFrames);
                uint8_t *output[] = {
                    reinterpret_cast<uint8_t *>(left.data()),
                    reinterpret_cast<uint8_t *>(right.data())};
                const int converted = swr_convert(
                    resampler, output, outputFrames,
                    const_cast<const uint8_t **>(frame->extended_data), frame->nb_samples);
                if (converted > 0 && !appendPcm(state, left.data(), right.data(), converted)) {
                    ended = true;
                    break;
                }
            }
            av_frame_unref(frame);
        }
    }

    if (resampler) swr_free(&resampler);
    if (frame) av_frame_free(&frame);
    if (packet) av_packet_free(&packet);
    if (codecContext) avcodec_free_context(&codecContext);
    closeInput(&input);
    if (isAborted(state)) setState(state, 2);
    else if (ended) setState(state, 2);
    else setState(state, -1, "FFmpeg stream decoder stopped");
}

class FfmpegStreamInstance final : public SoLoud::AudioSourceInstance {
public:
    explicit FfmpegStreamInstance(StreamState *state) : mState(state) {
        mBaseSamplerate = kOutputRate;
        mChannels = 2;
    }

    unsigned int getAudio(float *buffer, unsigned int frames, unsigned int bufferSize) override {
        const uint64_t read = mState->readFrame.load(std::memory_order_relaxed);
        const uint64_t write = mState->writeFrame.load(std::memory_order_acquire);
        const unsigned int count = static_cast<unsigned int>(
            std::min(write - read, static_cast<uint64_t>(frames)));
        for (unsigned int i = 0; i < count; ++i) {
            buffer[i] = mState->left[(read + i) % kPcmCapacity];
            buffer[bufferSize + i] = mState->right[(read + i) % kPcmCapacity];
        }
        mState->readFrame.store(read + count, std::memory_order_release);
        return count;
    }

    bool hasEnded() override {
        const int status = mState->status.load();
        return (status == 2 || status == -1) &&
               mState->readFrame.load() == mState->writeFrame.load();
    }

    SoLoud::result seek(SoLoud::time, float *, unsigned int) override { return SoLoud::FILE_LOAD_FAILED; }
    SoLoud::result rewind() override { return SoLoud::FILE_LOAD_FAILED; }

private:
    StreamState *mState;
};
}

namespace SoLoud {
FfmpegStreamSource::FfmpegStreamSource() : mSampleRate(kOutputRate), mState(new StreamState) {
    mBaseSamplerate = static_cast<float>(mSampleRate);
    mChannels = 2;
    try {
        static_cast<StreamState *>(mState)->decoder = std::thread([](StreamState *state) {
            try { decodeLoop(state); }
            catch (...) { setState(state, -1, "FFmpeg stream worker failed"); }
        }, static_cast<StreamState *>(mState));
    } catch (...) {
        delete static_cast<StreamState *>(mState);
        throw;
    }
}

FfmpegStreamSource::~FfmpegStreamSource() {
    abort();
    auto *state = static_cast<StreamState *>(mState);
    if (state->decoder.joinable()) state->decoder.join();
    delete state;
    mState = nullptr;
}

AudioSourceInstance *FfmpegStreamSource::createInstance() {
    return new (std::nothrow) FfmpegStreamInstance(static_cast<StreamState *>(mState));
}

int FfmpegStreamSource::write(const unsigned char *data, unsigned int length) {
    auto *state = static_cast<StreamState *>(mState);
    if (!data || length == 0) return 0;
    std::unique_lock<std::mutex> lock(state->inputMutex);
    if (state->inputClosed || state->aborted) return -1;
    const size_t available = kCompressedCapacity - state->compressed.size();
    const size_t count = std::min(available, static_cast<size_t>(length));
    state->compressed.insert(state->compressed.end(), data, data + count);
    lock.unlock();
    state->inputChanged.notify_all();
    return static_cast<int>(count);
}

void FfmpegStreamSource::closeInput() {
    auto *state = static_cast<StreamState *>(mState);
    {
        std::lock_guard<std::mutex> lock(state->inputMutex);
        state->inputClosed = true;
    }
    state->inputChanged.notify_all();
}

void FfmpegStreamSource::abort() {
    auto *state = static_cast<StreamState *>(mState);
    {
        std::lock_guard<std::mutex> lock(state->inputMutex);
        state->aborted = true;
        state->inputClosed = true;
    }
    state->inputChanged.notify_all();
    state->pcmChanged.notify_all();
    state->stateChanged.notify_all();
}

int FfmpegStreamSource::status() const {
    auto *state = static_cast<StreamState *>(mState);
    std::lock_guard<std::mutex> lock(state->stateMutex);
    return state->status;
}

unsigned int FfmpegStreamSource::bufferedFrames() const {
    auto *state = static_cast<StreamState *>(mState);
    const uint64_t read = state->readFrame.load();
    const uint64_t write = state->writeFrame.load();
    return static_cast<unsigned int>(write - read);
}

const char *FfmpegStreamSource::error() const {
    auto *state = static_cast<StreamState *>(mState);
    std::lock_guard<std::mutex> lock(state->stateMutex);
    static thread_local std::string copy;
    copy = state->error;
    return copy.c_str();
}
}

extern "C" {
void *FfmpegStream_create() {
    try { return new SoLoud::FfmpegStreamSource(); }
    catch (...) { return nullptr; }
}
void FfmpegStream_destroy(void *source) { delete static_cast<SoLoud::FfmpegStreamSource *>(source); }
int FfmpegStream_write(void *source, const unsigned char *data, unsigned int length) {
    return static_cast<SoLoud::FfmpegStreamSource *>(source)->write(data, length);
}
void FfmpegStream_closeInput(void *source) { static_cast<SoLoud::FfmpegStreamSource *>(source)->closeInput(); }
void FfmpegStream_abort(void *source) { static_cast<SoLoud::FfmpegStreamSource *>(source)->abort(); }
int FfmpegStream_status(void *source) { return static_cast<SoLoud::FfmpegStreamSource *>(source)->status(); }
unsigned int FfmpegStream_bufferedFrames(void *source) {
    return static_cast<SoLoud::FfmpegStreamSource *>(source)->bufferedFrames();
}
const char *FfmpegStream_error(void *source) { return static_cast<SoLoud::FfmpegStreamSource *>(source)->error(); }
}
