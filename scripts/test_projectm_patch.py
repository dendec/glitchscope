"""Compile and exercise the projectM patch in the Docker builder, without a GPU."""
import pathlib
import shutil
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]


class ProjectMShaderTest(unittest.TestCase):
    def test_shader_pipeline(self):
        self.run_native("test-projectm-shader.cpp")

    def test_worker_pipeline(self):
        self.run_native("test-projectm-worker.cpp")

    def run_native(self, test_source):
        with tempfile.TemporaryDirectory() as directory:
            work = pathlib.Path(directory)
            source = ROOT / "lib/projectm"
            # Apply the patch in a scratch copy so the submodule stays clean.
            shutil.copytree(source / "src", work / "src")
            subprocess.run(
                ["git", "apply", str(ROOT / "patches/projectm-async-preset.patch")],
                cwd=work, check=True,
            )
            binary = work / "test-shader"
            subprocess.run([
                "g++", "-std=c++17", "-fsanitize=address,undefined", "-fno-omit-frame-pointer", "-I" + str(work / "src/libprojectM"),
                "-I" + str(source / "vendor"),
                str(ROOT / "scripts" / test_source),
                str(work / "src/libprojectM/Renderer/Shader.cpp"),
                "-pthread", "-lGL", "-o", str(binary),
            ], check=True)
            subprocess.run([str(binary)], check=True, timeout=15)


if __name__ == "__main__":
    unittest.main()
