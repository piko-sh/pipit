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

package extract

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiscoverFollowsProjectImports(t *testing.T) {
	t.Parallel()

	root := writeFixtureProject(t, fixtureFiles{
		"go.mod":          fixtureModule,
		"pages/home.tmpl": "<script>\npackage main\n\nimport \"example.com/fixture/internal/site\"\n\nvar _ = site.Name\n</script>\n",
		"internal/site/site.go": "package site\n\nimport (\n\t\"expvar\"\n\t\"example.com/fixture/internal/util\"\n)\n\n" +
			"var _ = expvar.Do\n\nvar Name = util.Name\n",
		"internal/site/content_native.go": "//go:build !interp\n\npackage site\n\nimport _ \"embed\"\n\n//go:embed site.go\nvar content string\n",
		"internal/site/content_interp.go": "//go:build interp\n\npackage site\n\nimport \"os\"\n\nvar content, _ = os.ReadFile(\"site.go\")\n",
		"internal/site/site_test.go":      "package site\n\nimport \"testing\"\n\nfunc TestName(t *testing.T) {}\n",
		"internal/site/site_plan9.go":     "package site\n\nimport \"syscall\"\n\nvar _ = syscall.Getpid\n",
		"internal/util/util.go":           "package util\n\nimport \"log/syslog\"\n\nvar _ = syslog.LOG_INFO\n\nconst Name = \"util\"\n",
		"internal/unused/unused.go":       "package unused\n\nimport \"net/rpc\"\n\nvar _ = rpc.Accept\n",
	})

	options := DiscoverOptions{
		Root:                 root,
		SourceDirs:           []string{"pages"},
		Scanners:             []SourceScanner{scriptBlockScanner{}},
		FollowProjectImports: true,
	}
	result, err := Discover(t.Context(), options)
	require.NoError(t, err)
	require.Equal(t, []string{"expvar", "log/syslog", "os"}, result.RequiredImports,
		"followed imports surface; project packages, !interp files and tests do not")
	require.Empty(t, result.Warnings)

	options.FollowProjectImports = false
	result, err = Discover(t.Context(), options)
	require.NoError(t, err)
	require.Equal(t, []string{"example.com/fixture/internal/site"}, result.RequiredImports)
}

func TestDiscoverFollowWarnsWithoutModulePath(t *testing.T) {
	t.Parallel()

	root := writeFixtureProject(t, fixtureFiles{
		"go.mod":  "go 1.22\n",
		"main.go": "package main\n\nimport \"expvar\"\n\nvar _ = expvar.Do\n",
	})

	result, err := Discover(t.Context(), DiscoverOptions{Root: root, FollowProjectImports: true})
	require.NoError(t, err)
	require.NotEmpty(t, result.Warnings)
	require.Contains(t, result.Warnings[0], "cannot follow project imports")
}

func TestDiscoverFollowSkipsProvidedAndIgnoredProjectPackages(t *testing.T) {
	t.Parallel()

	root := writeFixtureProject(t, fixtureFiles{
		"go.mod": fixtureModule,
		"pages/home.tmpl": "<script>\npackage main\n\nimport (\n\t\"example.com/fixture/internal/provided\"\n" +
			"\t\"example.com/fixture/internal/ignored\"\n\t\"example.com/fixture/internal/site\"\n)\n</script>\n",
		"internal/provided/p.go": "package provided\n\nimport \"expvar\"\n\nvar _ = expvar.Do\n",
		"internal/ignored/i.go":  "package ignored\n\nimport \"net/rpc\"\n\nvar _ = rpc.Accept\n",
		"internal/site/s.go":     "package site\n\nimport (\n\t\"log/syslog\"\n\t\"example.com/fixture/internal/provided\"\n)\n\nvar _, _ = syslog.LOG_INFO, provided.X\n",
	})

	result, err := Discover(t.Context(), DiscoverOptions{
		Root:                 root,
		SourceDirs:           []string{"pages"},
		Scanners:             []SourceScanner{scriptBlockScanner{}},
		AlreadyProvided:      []string{"example.com/fixture/internal/provided"},
		ExtraIgnored:         []string{"example.com/fixture/internal/ignored"},
		FollowProjectImports: true,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"log/syslog"}, result.RequiredImports,
		"a provided or ignored project package is not compiled from source, so its imports are not needed")
	require.Empty(t, result.Warnings)
}

func TestDiscoverFollowKeepsImportsOfReadableFiles(t *testing.T) {
	t.Parallel()

	root := writeFixtureProject(t, fixtureFiles{
		"go.mod":               fixtureModule,
		"pages/home.tmpl":      "<script>\npackage main\n\nimport \"example.com/fixture/internal/site\"\n</script>\n",
		"internal/site/ok.go":  "package site\n\nimport \"expvar\"\n\nvar _ = expvar.Do\n",
		"internal/site/bad.go": "package site\n\nimport (\n",
	})

	result, err := Discover(t.Context(), DiscoverOptions{
		Root:                 root,
		SourceDirs:           []string{"pages"},
		Scanners:             []SourceScanner{scriptBlockScanner{}},
		FollowProjectImports: true,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"expvar"}, result.RequiredImports, "one unreadable file does not hide the others")
	require.Len(t, result.Warnings, 1)
	require.Contains(t, result.Warnings[0], "internal/site/bad.go")
}

func TestDiscoverFollowWarnsOnUnreadableProjectPackage(t *testing.T) {
	t.Parallel()

	root := writeFixtureProject(t, fixtureFiles{
		"go.mod":           fixtureModule,
		"pages/home.tmpl":  "<script>\npackage main\n\nimport \"example.com/fixture/internal/missing\"\n</script>\n",
		"internal/doc.txt": "not a package",
	})

	result, err := Discover(t.Context(), DiscoverOptions{
		Root:                 root,
		SourceDirs:           []string{"pages"},
		Scanners:             []SourceScanner{scriptBlockScanner{}},
		FollowProjectImports: true,
	})
	require.NoError(t, err)
	require.Empty(t, result.RequiredImports)
	require.Len(t, result.Warnings, 1)
	require.Contains(t, result.Warnings[0], "example.com/fixture/internal/missing")
}

func TestDiscoverFollowWarnsWhenNoFileIsCompiled(t *testing.T) {
	t.Parallel()

	root := writeFixtureProject(t, fixtureFiles{
		"go.mod":              fixtureModule,
		"pages/home.tmpl":     "<script>\npackage main\n\nimport \"example.com/fixture/internal/cgo\"\n</script>\n",
		"internal/cgo/cgo.go": "package cgo\n\nimport (\n\t\"C\"\n\t\"expvar\"\n)\n\nvar _ = expvar.Do\n",
	})

	result, err := Discover(t.Context(), DiscoverOptions{
		Root:                 root,
		SourceDirs:           []string{"pages"},
		Scanners:             []SourceScanner{scriptBlockScanner{}},
		FollowProjectImports: true,
	})
	require.NoError(t, err)
	require.Empty(t, result.RequiredImports)
	require.Len(t, result.Warnings, 1)
	require.Contains(t, result.Warnings[0], "example.com/fixture/internal/cgo has no Go files the interpreter compiles")
}

func TestFollowProjectImportsStopsWhenCancelled(t *testing.T) {
	t.Parallel()

	root := writeFixtureProject(t, fixtureFiles{
		"go.mod":                fixtureModule,
		"internal/site/site.go": "package site\n\nimport \"expvar\"\n\nvar _ = expvar.Do\n",
	})
	ctx, cancel := context.WithCancelCause(t.Context())
	cause := errors.New("shutting down")
	cancel(cause)

	sourceImports := map[string]struct{}{"example.com/fixture/internal/site": {}}
	_, err := followProjectImports(ctx, root, "example.com/fixture", DefaultFileFilter(nil), nil, sourceImports)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, cause)
}
