// A blocked GL driver must never block render-thread polling or cancellation.
#include "Renderer/Shader.hpp"
#include <atomic>
#include <cassert>
#include <chrono>
#include <condition_variable>
#include <cstring>
#include <iostream>
#include <mutex>
#include <stdexcept>
#include <string>
#include <thread>
#include <unordered_set>
#include <utility>

using libprojectM::Renderer::Shader;
static const auto renderThread = std::this_thread::get_id();
static std::mutex mutex;
static std::condition_variable wake;
static bool blocked = true;
static bool entered = false;
static std::atomic<bool> failCompile{false};
static std::atomic<bool> failLink{false};
static std::atomic<int> compiles{0};
static std::atomic<int> finishes{0};
static std::atomic<GLuint> nextID{1};
static std::unordered_set<GLuint> programs, shaders;

extern "C" {
void glGetIntegerv(GLenum name, GLint* value) { assert(name == GL_NUM_EXTENSIONS); *value = 0; }
const GLubyte* glGetStringi(GLenum, GLuint) { assert(false); return nullptr; }
GLuint glCreateProgram() {
    assert(std::this_thread::get_id() != renderThread);
    std::lock_guard<std::mutex> lock(mutex);
    GLuint id = nextID++;
    programs.insert(id);
    return id;
}
GLuint glCreateShader(GLenum) {
    assert(std::this_thread::get_id() != renderThread);
    std::lock_guard<std::mutex> lock(mutex);
    GLuint id = nextID++;
    shaders.insert(id);
    return id;
}
void glShaderSource(GLuint, GLsizei, const GLchar* const*, const GLint*) {}
void glCompileShader(GLuint) {
    assert(std::this_thread::get_id() != renderThread);
    ++compiles;
    std::unique_lock<std::mutex> lock(mutex);
    entered = true;
    wake.notify_all();
    wake.wait(lock, [] { return !blocked; });
}
void glAttachShader(GLuint, GLuint) {}
void glLinkProgram(GLuint) { assert(std::this_thread::get_id() != renderThread); }
void glDetachShader(GLuint, GLuint) {}
void glDeleteShader(GLuint id) { std::lock_guard<std::mutex> lock(mutex); assert(shaders.erase(id) == 1); }
void glDeleteProgram(GLuint id) { std::lock_guard<std::mutex> lock(mutex); assert(programs.erase(id) == 1); }
void glGetShaderiv(GLuint, GLenum name, GLint* value) {
    assert(std::this_thread::get_id() != renderThread);
    assert(name != 0x91B1); // No parallel-compile extension on this driver.
    *value = name == GL_INFO_LOG_LENGTH ? 0 : !failCompile;
}
void glGetProgramiv(GLuint, GLenum name, GLint* value) {
    assert(std::this_thread::get_id() != renderThread);
    assert(name != 0x91B1);
    *value = name == GL_INFO_LOG_LENGTH ? 0 : !failLink;
}
void glGetShaderInfoLog(GLuint, GLsizei, GLsizei*, GLchar*) {}
void glGetProgramInfoLog(GLuint, GLsizei, GLsizei*, GLchar*) {}
void glFinish() { assert(std::this_thread::get_id() != renderThread); ++finishes; }
}

static void submit(Shader& shader) {
    Shader::SetParallelCompileEnabled(true);
    shader.CompileProgram("vertex", "fragment");
    Shader::SetParallelCompileEnabled(false);
}

static void awaitReady(Shader& shader) {
    auto deadline = std::chrono::steady_clock::now() + std::chrono::seconds(5);
    while (!shader.Ready()) {
        assert(std::chrono::steady_clock::now() < deadline);
        std::this_thread::yield();
    }
}

int main() {
    Shader::EnableWorker(true);
    assert(std::strcmp(Shader::CompileMode(), "shared-context") == 0);
    std::thread worker(Shader::RunWorker);
    {
        auto cancelled = std::make_unique<Shader>();
        submit(*cancelled);
        {
            std::unique_lock<std::mutex> lock(mutex);
            assert(wake.wait_for(lock, std::chrono::seconds(5), [] { return entered; }));
        }
        // Simulate UI ticks while the driver cannot make progress.
        for (int i = 0; i < 1000; ++i) assert(!cancelled->Ready());
        cancelled.reset();
        // Cancel queued requests too: none should reach the driver.
        for (int i = 0; i < 100; ++i) { Shader stale; submit(stale); }
        Shader latest;
        submit(latest);
        assert(!latest.Ready());
        {
            std::lock_guard<std::mutex> lock(mutex);
            blocked = false;
        }
        wake.notify_all();
        awaitReady(latest);
        assert(compiles == 4); // In-flight cancelled job + latest, no stale queue.
        assert(finishes == 2);

        std::atomic<bool> sourceEntered{false};
        std::atomic<bool> sourceReleased{false};
        std::atomic<bool> sourceRanOnWorker{false};
        Shader deferred;
        Shader::SetParallelCompileEnabled(true);
        deferred.CompileProgramDeferred([&] {
            sourceRanOnWorker = std::this_thread::get_id() != renderThread;
            sourceEntered = true;
            while (!sourceReleased.load()) std::this_thread::yield();
            return std::make_pair(std::string("deferred vertex"), std::string("deferred fragment"));
        });
        Shader::SetParallelCompileEnabled(false);
        assert(!sourceEntered.load());
        auto sourceDeadline = std::chrono::steady_clock::now() + std::chrono::seconds(5);
        while (!sourceEntered.load()) {
            assert(std::chrono::steady_clock::now() < sourceDeadline);
            assert(!deferred.Ready());
            std::this_thread::yield();
        }
        // CPU-side source preparation must leave render-thread polling nonblocking too.
        for (int i = 0; i < 1000; ++i) assert(!deferred.Ready());
        sourceReleased = true;
        awaitReady(deferred);
        assert(sourceRanOnWorker.load());

        Shader sourceFailure;
        Shader::SetParallelCompileEnabled(true);
        sourceFailure.CompileProgramDeferred([]() -> std::pair<std::string, std::string> {
            throw std::runtime_error("source factory failure");
        });
        Shader::SetParallelCompileEnabled(false);
        auto failureDeadline = std::chrono::steady_clock::now() + std::chrono::seconds(5);
        bool sourceErrorCaught = false;
        while (!sourceErrorCaught) {
            try {
                assert(!sourceFailure.Ready());
            }
            catch (const libprojectM::Renderer::ShaderException&) {
                sourceErrorCaught = true;
            }
            assert(sourceErrorCaught || std::chrono::steady_clock::now() < failureDeadline);
            std::this_thread::yield();
        }

        for (bool linkFailure : {false, true}) {
            failCompile = !linkFailure;
            failLink = linkFailure;
            Shader failed;
            submit(failed);
            bool caught = false;
            try { awaitReady(failed); }
            catch (const libprojectM::Renderer::ShaderException&) { caught = true; }
            assert(caught);
        }
    }
    Shader::StopWorker();
    worker.join();
    assert(programs.empty() && shaders.empty());
    // Stop must not wait for a blocked driver, and queued jobs are discarded.
    {
        blocked = true;
        entered = false;
        failCompile = false;
        failLink = false;
        Shader::EnableWorker(true);
        std::thread second(Shader::RunWorker);
        Shader running;
        submit(running);
        {
            std::unique_lock<std::mutex> lock(mutex);
            assert(wake.wait_for(lock, std::chrono::seconds(5), [] { return entered; }));
        }
        Shader queued;
        submit(queued);
        Shader::StopWorker();
        {
            std::lock_guard<std::mutex> lock(mutex);
            blocked = false;
        }
        wake.notify_all();
        second.join();
        assert(!queued.Ready());
    }
    assert(programs.empty() && shaders.empty());
    // Restart and shutdown with an empty queue must also wake correctly.
    Shader::EnableWorker(true);
    std::thread third(Shader::RunWorker);
    Shader::StopWorker();
    third.join();
    std::cout << "projectM shared-context worker regression passed\n";
}
