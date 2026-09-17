#ifndef GLITCHSCOPE_GL_COMPAT_H
#define GLITCHSCOPE_GL_COMPAT_H

// Keep the C helpers source-compatible across the GLES2 Linux/ARM builds and
// the desktop OpenGL Windows build.  The call surface used by GlitchScope is
// deliberately limited to the common shader/texture/framebuffer subset.
#if defined(_WIN32)
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>
#if defined(GLITCHSCOPE_GLEW_STATIC)
#define GLEW_STATIC
#endif
#include <GL/glew.h>
#else
#include <GLES2/gl2.h>
#include <GLES2/gl2ext.h>
#endif

#endif
