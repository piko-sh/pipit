// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

//go:build integration && crossarch

package language_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type crossArchTarget struct {
	goarch   string
	platform string
}

var (
	crossArchTargets = []crossArchTarget{
		{goarch: "amd64", platform: "linux/amd64"},
		{goarch: "arm64", platform: "linux/arm64"},
	}
)

func TestCrossArch(t *testing.T) {
	dockerBin, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("docker not found in PATH, skipping cross-arch tests")
	}

	pkgDir := findPackageDir(t)

	for _, target := range crossArchTargets {
		t.Run(target.goarch, func(t *testing.T) {
			if runtime.GOARCH == target.goarch {
				t.Skipf("already running on %s, covered by regular tests", target.goarch)
			}

			runCrossArch(t, dockerBin, pkgDir, target)
		})
	}
}

func runCrossArch(t *testing.T, dockerBin, pkgDir string, target crossArchTarget) {
	t.Helper()

	binName := "engine_" + target.goarch + ".test"
	binPath := filepath.Join(t.TempDir(), binName)

	build := exec.Command("go", "test", "-tags", "integration", "-c", "-o", binPath, ".")
	build.Dir = pkgDir
	build.Env = append(os.Environ(), "GOARCH="+target.goarch, "CGO_ENABLED=0")

	buildOutput, err := build.CombinedOutput()
	require.NoError(t, err, "cross-compile for %s:\n%s", target.goarch, buildOutput)

	gorootOutput, err := exec.Command("go", "env", "GOROOT").Output()
	require.NoError(t, err, "go env reports GOROOT")
	goroot := strings.TrimSpace(string(gorootOutput))
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(pkgDir)))
	run := exec.Command(
		dockerBin, "run", "--rm", "--platform", target.platform,
		"-v", binPath+":/test:ro",
		"-v", goroot+":"+goroot+":ro",
		"-v", repoRoot+":"+repoRoot+":ro",
		"-e", "GOROOT="+goroot,
		"-e", "PATH="+goroot+"/bin:/usr/bin:/bin",
		"-w", pkgDir,
		"alpine:latest",
		"/test", "-test.v", "-test.short",
		"-test.run", "^TestEval|^TestDispatch|^TestCallFrame|^TestVarLocation|^TestASM|^TestAsm",
	)

	out, err := run.CombinedOutput()
	output := string(out)
	t.Log(output)

	if err != nil {
		require.NotContains(t, output, "exec format error", "QEMU binfmt is registered for %s; to register it, run\n"+
			"  docker run --rm --privileged multiarch/qemu-user-static --reset -p yes", target.goarch)
		require.Failf(t, "cross-architecture tests failed", "%s:\n%s", target.goarch, output)
	}

	require.Contains(t, output, "PASS")
}

func findPackageDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Dir(file)
}
