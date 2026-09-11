// Run by scripts/dtest.sh native-radio inside the builder image.
#include "../internal/soloud/stream_source.h"
#include <cassert>
#include <chrono>
#include <cstdint>
#include <cstring>
#include <thread>
#include <vector>

int main() {
    using Clock = std::chrono::steady_clock;
    SoLoud::FfmpegStreamSource source;
    auto *instance = source.createInstance();
    assert(instance);
    instance->init(source, 0);
    assert(instance->mChannels == 2); // base AudioSource channels, not a shadow
    float pcm[2048]{};
    auto start = Clock::now();
    assert(instance->getAudio(pcm, 1024, 1024) == 0);
    assert(Clock::now() - start < std::chrono::milliseconds(100));

    // Eight seconds of opposite-polarity stereo PCM exercise wrap-around and
    // full-buffer backpressure, as well as channel separation through FFmpeg.
    const uint32_t frames = 44100 * 8;
    std::vector<unsigned char> wav(44 + frames * 4);
    auto put16 = [&](int offset, uint16_t value) {
        wav[offset] = value & 255; wav[offset + 1] = value >> 8;
    };
    auto put32 = [&](int offset, uint32_t value) {
        for (int i = 0; i < 4; ++i) wav[offset + i] = (value >> (i * 8)) & 255;
    };
    std::memcpy(wav.data(), "RIFF", 4);
    put32(4, wav.size() - 8);
    std::memcpy(wav.data() + 8, "WAVEfmt ", 8);
    put32(16, 16); put16(20, 1); put16(22, 2);
    put32(24, 44100); put32(28, 44100 * 4); put16(32, 4); put16(34, 16);
    std::memcpy(wav.data() + 36, "data", 4); put32(40, frames * 4);
    for (uint32_t i = 0; i < frames; ++i) {
        put16(44 + i * 4, 8192); put16(46 + i * 4, static_cast<uint16_t>(-8192));
    }
    assert(source.write(wav.data(), wav.size()) == static_cast<int>(wav.size()));
    source.closeInput();
    start = Clock::now();
    while (source.bufferedFrames() < 44100 * 3 && Clock::now() - start < std::chrono::seconds(5)) {
        std::this_thread::sleep_for(std::chrono::milliseconds(1));
    }
    assert(source.bufferedFrames() >= 44100 * 3);
    assert(source.bufferedFrames() <= 44100 * 4);
    uint32_t consumed = 0;
    while (!instance->hasEnded() && Clock::now() - start < std::chrono::seconds(10)) {
        auto n = instance->getAudio(pcm, 1024, 1024);
        for (unsigned int i = 0; i < n; ++i) {
            assert(pcm[i] > 0.24f && pcm[i] < 0.26f);
            assert(pcm[1024 + i] < -0.24f && pcm[1024 + i] > -0.26f);
        }
        consumed += n;
        if (!n) std::this_thread::sleep_for(std::chrono::milliseconds(1));
    }
    assert(consumed == frames);
    delete instance;

    // Abort while custom AVIO is waiting for its first byte.
    start = Clock::now();
    { SoLoud::FfmpegStreamSource stalled; stalled.abort(); }
    assert(Clock::now() - start < std::chrono::seconds(1));
}
