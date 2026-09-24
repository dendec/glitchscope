package projectm

/*
#cgo windows CFLAGS: -DGLITCHSCOPE_GLEW_STATIC
#include "../gl/compat.h"

static GLuint shaderShareProbe(void) {
	GLuint program = glCreateProgram();
	glFinish();
	return program;
}
static int shaderShareVisible(GLuint program) { return glIsProgram(program); }
static void shaderShareDelete(GLuint program) { glDeleteProgram(program); }
*/
import "C"

// CreateShareProbe creates an object to verify sharing before starting workers.
// The main context must be current.
func CreateShareProbe() uint32 { return uint32(C.shaderShareProbe()) }

// ShareProbeVisible checks the probe with the worker context current.
func ShareProbeVisible(program uint32) bool { return C.shaderShareVisible(C.GLuint(program)) != 0 }

// DeleteShareProbe deletes the probe with either shared context current.
func DeleteShareProbe(program uint32) { C.shaderShareDelete(C.GLuint(program)) }
