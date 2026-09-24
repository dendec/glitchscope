// Driver-independent regression for the patched projectM shader load path.
#include "Renderer/Shader.hpp"
#include <cassert>
#include <cstring>
#include <iostream>
#include <vector>

static std::vector<const char*> extensions;
static bool complete = false;
static bool fail = false;
static int statusQueries = 0;
static int deletedShaders = 0;

extern "C" {
const GLubyte* glGetString(GLenum) { assert(false && "legacy extension query"); return nullptr; }
void glGetIntegerv(GLenum name, GLint* value) {
    assert(name == GL_NUM_EXTENSIONS);
    *value = static_cast<GLint>(extensions.size());
}
const GLubyte* glGetStringi(GLenum name, GLuint index) {
    assert(name == GL_EXTENSIONS);
    return reinterpret_cast<const GLubyte*>(extensions.at(index));
}
GLuint glCreateProgram() { return 1; }
GLuint glCreateShader(GLenum) { static GLuint id = 1; return ++id; }
void glShaderSource(GLuint, GLsizei, const GLchar* const*, const GLint*) {}
void glCompileShader(GLuint) {}
void glAttachShader(GLuint, GLuint) {}
void glLinkProgram(GLuint) {}
void glDetachShader(GLuint, GLuint) {}
void glDeleteShader(GLuint) { ++deletedShaders; }
void glDeleteProgram(GLuint) {}
void glGetShaderiv(GLuint, GLenum name, GLint* value) {
    if (name == 0x91B1) { *value = complete; return; }
    ++statusQueries;
    assert(complete && "blocking shader query before completion");
    *value = name == GL_INFO_LOG_LENGTH ? 0 : !fail;
}
void glGetProgramiv(GLuint, GLenum name, GLint* value) {
    if (name == 0x91B1) { *value = complete; return; }
    ++statusQueries;
    assert(complete && "blocking program query before completion");
    *value = GL_TRUE;
}
void glGetShaderInfoLog(GLuint, GLsizei, GLsizei*, GLchar*) {}
}

int main() {
    using libprojectM::Renderer::Shader;
    for (const char* mode : {"ARB", "KHR"}) {
        extensions = {std::strcmp(mode, "ARB") == 0 ? "GL_ARB_parallel_shader_compile" : "GL_KHR_parallel_shader_compile"};
        assert(std::strcmp(Shader::ParallelCompileExtension(), mode) == 0);
        complete = false;
        statusQueries = 0;
        Shader shader;
        Shader::SetParallelCompileEnabled(true);
        shader.CompileProgram("vertex", "fragment");
        Shader::SetParallelCompileEnabled(false);
        assert(statusQueries == 0);
        assert(!Shader::AllReady());
        assert(statusQueries == 0);
        complete = true;
        assert(Shader::AllReady());
        assert(statusQueries == 3);
    }
    // Similar names must not enable unsupported GL tokens.
    extensions = {"GL_ARB_parallel_shader_compile_other"};
    assert(std::strcmp(Shader::ParallelCompileExtension(), "") == 0);
    complete = true;
    statusQueries = 0;
    {
        Shader shader;
        Shader::SetParallelCompileEnabled(true);
        shader.CompileProgram("vertex", "fragment");
        Shader::SetParallelCompileEnabled(false);
        assert(statusQueries == 3);
    }
    extensions = {"GL_ARB_parallel_shader_compile"};
    // Cancelling an unfinished program must release its shader objects.
    int before = deletedShaders;
    {
        complete = false;
        Shader shader;
        Shader::SetParallelCompileEnabled(true);
        shader.CompileProgram("vertex", "fragment");
        Shader::SetParallelCompileEnabled(false);
    }
    assert(deletedShaders == before + 2);
    assert(Shader::AllReady());
    // A failed compile is reported only once the job has completed.
    {
        Shader shader;
        Shader::SetParallelCompileEnabled(true);
        shader.CompileProgram("vertex", "fragment");
        Shader::SetParallelCompileEnabled(false);
        complete = true;
        fail = true;
        bool caught = false;
        try { Shader::AllReady(); }
        catch (const libprojectM::Renderer::ShaderException&) { caught = true; }
        assert(caught);
    }
    assert(Shader::AllReady());
    std::cout << "projectM asynchronous shader regression passed\n";
}
