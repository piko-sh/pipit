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

package app_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"pipit.sh/pipit/internal/app"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/adapters"
	"pipit.sh/pipit/internal/module"
	"pipit.sh/pipit/internal/symtab"
)

func TestLoadModule_BytecodeBridgesSymbols(t *testing.T) {
	t.Parallel()

	libSources := map[string]map[string]string{
		"": {
			"lib.go": `package lib

func Add(a, b int) int {
	return a + b
}
`,
		},
	}

	builder := app.NewService()
	descriptor := module.Descriptor{
		SchemaVersion: module.DescriptorVersion,
		Ref:           module.Ref{Path: "example.com/lib", Version: "v0.0.0"},
	}
	bundle, err := builder.PackageModule(context.Background(),
		descriptor,
		"example.com/lib",
		libSources,
		adapters.PackCompiledFileSetToBytes,
	)
	require.NoError(t, err)
	require.NotEmpty(t, bundle.Bytecode, "bundle should carry bytecode")
	require.NotEmpty(t, bundle.TypesExport, "bundle should carry TypesExport so downstream imports resolve")

	consumer := app.NewService()
	reference := module.Ref{
		Path:    descriptor.Ref.Path,
		Version: descriptor.Ref.Version,
		Pin:     bundle.Descriptor.Ref.Pin,
	}
	loaded, err := consumer.LoadModule(context.Background(), bundle, reference, nil, adapters.LoadCompiledFromBytes)
	require.NoError(t, err)
	require.Equal(t, bundle.Descriptor.Ref.Pin, loaded.Fingerprint)

	mainSources := map[string]map[string]string{
		"": {
			"main.go": `package main

import "example.com/lib"

func entrypoint() int {
	return lib.Add(2, 5)
}

func main() {}
`,
		},
	}
	mainCfs, err := consumer.CompileProgram(context.Background(), "main", mainSources)
	require.NoError(t, err, "main should compile against the bytecode-loaded lib")

	result, err := consumer.ExecuteEntrypoint(context.Background(), mainCfs, "entrypoint")
	require.NoError(t, err)
	require.Equal(t, "7", fmt.Sprint(result))
}

func TestLoadModule_MissingTypesExport_DownstreamCompileFails(t *testing.T) {
	t.Parallel()

	libSources := map[string]map[string]string{
		"": {
			"lib.go": `package lib

func Greet() string { return "hi" }
`,
		},
	}

	builder := app.NewService()
	descriptor := module.Descriptor{
		SchemaVersion: module.DescriptorVersion,
		Ref:           module.Ref{Path: "example.com/lib2", Version: "v0.0.0"},
	}
	bundle, err := builder.PackageModule(context.Background(),
		descriptor,
		"example.com/lib2",
		libSources,
		adapters.PackCompiledFileSetToBytes,
	)
	require.NoError(t, err)

	bundle.TypesExport = nil
	fingerprint, err := bundle.Fingerprint()
	require.NoError(t, err)
	bundle.Descriptor.Ref.Pin = fingerprint

	consumer := app.NewService()
	reference := module.Ref{
		Path:    descriptor.Ref.Path,
		Version: descriptor.Ref.Version,
		Pin:     fingerprint,
	}
	_, err = consumer.LoadModule(context.Background(), bundle, reference, nil, adapters.LoadCompiledFromBytes)
	require.NoError(t, err, "LoadModule should still succeed for legacy bundles (no TypesExport)")

	mainSources := map[string]map[string]string{
		"": {
			"main.go": `package main

import "example.com/lib2"

func entrypoint() string { return lib2.Greet() }

func main() {}
`,
		},
	}
	_, err = consumer.CompileProgram(context.Background(), "main", mainSources)
	require.Error(t, err, "CompileProgram should fail when importing a bundle without TypesExport")
}

func TestLoadModule_VarExportRoundtrip(t *testing.T) {
	t.Parallel()

	libSources := map[string]map[string]string{
		"": {
			"lib.go": `package varlib

var Answer = 40 + 2

var Greeting = "hello"
`,
		},
	}

	builder := app.NewService()
	descriptor := module.Descriptor{
		SchemaVersion: module.DescriptorVersion,
		Ref:           module.Ref{Path: "example.com/varlib", Version: "v0.0.0"},
	}
	bundle, err := builder.PackageModule(context.Background(),
		descriptor,
		"example.com/varlib",
		libSources,
		adapters.PackCompiledFileSetToBytes,
	)
	require.NoError(t, err)
	require.NotEmpty(t, bundle.Bytecode)
	require.NotEmpty(t, bundle.TypesExport)

	consumer := app.NewService()
	reference := module.Ref{
		Path:    descriptor.Ref.Path,
		Version: descriptor.Ref.Version,
		Pin:     bundle.Descriptor.Ref.Pin,
	}
	_, err = consumer.LoadModule(context.Background(), bundle, reference, nil, adapters.LoadCompiledFromBytes)
	require.NoError(t, err)

	mainSources := map[string]map[string]string{
		"": {
			"main.go": `package main

import "example.com/varlib"

func entrypoint() int {
	return varlib.Answer + len(varlib.Greeting)
}

func main() {}
`,
		},
	}
	mainCfs, err := consumer.CompileProgram(context.Background(), "main", mainSources)
	require.NoError(t, err)

	require.NoError(t, consumer.ExecuteInits(context.Background(), mainCfs))

	result, err := consumer.ExecuteEntrypoint(context.Background(), mainCfs, "entrypoint")
	require.NoError(t, err)

	require.Equal(t, "47", fmt.Sprint(result))
}

func TestLoadModule_VarSlotsDoNotCollide(t *testing.T) {
	t.Parallel()

	makeLib := func(value int) ([]byte, []byte, string, string) {
		libSources := map[string]map[string]string{
			"": {
				"lib.go": fmt.Sprintf(`package twolib%d

var Counter = %d
`, value, value),
			},
		}
		builder := app.NewService()
		path := fmt.Sprintf("example.com/twolib%d", value)
		descriptor := module.Descriptor{
			SchemaVersion: module.DescriptorVersion,
			Ref:           module.Ref{Path: path, Version: "v0.0.0"},
		}
		bundle, err := builder.PackageModule(context.Background(),
			descriptor, path, libSources, adapters.PackCompiledFileSetToBytes,
		)
		require.NoError(t, err)
		return bundle.Bytecode, bundle.TypesExport, path, bundle.Descriptor.Ref.Pin
	}

	bcA, teA, pathA, pinA := makeLib(10)
	bcB, teB, pathB, pinB := makeLib(20)

	consumer := app.NewService()
	bundleA := &module.Bundle{
		Descriptor:  &module.Descriptor{SchemaVersion: module.DescriptorVersion, Ref: module.Ref{Path: pathA, Version: "v0.0.0"}},
		Bytecode:    bcA,
		TypesExport: teA,
	}
	bundleA.Descriptor.Ref.Pin = pinA
	bundleB := &module.Bundle{
		Descriptor:  &module.Descriptor{SchemaVersion: module.DescriptorVersion, Ref: module.Ref{Path: pathB, Version: "v0.0.0"}},
		Bytecode:    bcB,
		TypesExport: teB,
	}
	bundleB.Descriptor.Ref.Pin = pinB

	_, err := consumer.LoadModule(context.Background(), bundleA, module.Ref{Path: pathA, Version: "v0.0.0", Pin: pinA}, nil, adapters.LoadCompiledFromBytes)
	require.NoError(t, err)
	_, err = consumer.LoadModule(context.Background(), bundleB, module.Ref{Path: pathB, Version: "v0.0.0", Pin: pinB}, nil, adapters.LoadCompiledFromBytes)
	require.NoError(t, err)

	mainSources := map[string]map[string]string{
		"": {
			"main.go": fmt.Sprintf(`package main

import (
	"%s"
	"%s"
)

func entrypoint() int {
	return twolib10.Counter*100 + twolib20.Counter
}

func main() {}
`, pathA, pathB),
		},
	}
	mainCfs, err := consumer.CompileProgram(context.Background(), "main", mainSources)
	require.NoError(t, err)

	require.NoError(t, consumer.ExecuteInits(context.Background(), mainCfs))

	result, err := consumer.ExecuteEntrypoint(context.Background(), mainCfs, "entrypoint")
	require.NoError(t, err)

	require.Equal(t, "1020", fmt.Sprint(result))
}

type hostNamedTypeProvider struct{}

func (hostNamedTypeProvider) Exports() symtab.SymbolExports {
	return symtab.SymbolExports{
		"fmt": {
			"Sprint":   reflect.ValueOf(fmt.Sprint),
			"Stringer": reflect.ValueOf((*fmt.Stringer)(nil)),
		},
		"time": {
			"Duration": reflect.ValueOf((*time.Duration)(nil)),
			"Second":   reflect.ValueOf(time.Second),
		},
		"hostprobe": {
			"Sum": reflect.ValueOf(func(durations []time.Duration) time.Duration {
				var total time.Duration
				for _, duration := range durations {
					total += duration
				}
				return total
			}),
			"TypeOf": reflect.ValueOf(func(value any) string { return fmt.Sprintf("%T", value) }),
		},
	}
}

func TestLoadModule_HostNamedTypeMethodsSurviveBundle(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		lib   string
		entry string
		want  string
	}{
		{
			name:  "direct",
			lib:   "import \"time\"\n\nfunc Format(d time.Duration) string { return d.String() }",
			entry: "lib.Format(2 * time.Second)",
			want:  "2s",
		},
		{
			name:  "method_value",
			lib:   "import \"time\"\n\nfunc Format(d time.Duration) string { f := d.String; return f() }",
			entry: "lib.Format(2 * time.Second)",
			want:  "2s",
		},
		{
			name:  "interface",
			lib:   "import \"fmt\"\nimport \"time\"\n\nfunc Format(d time.Duration) string { var s fmt.Stringer = d; return s.String() }",
			entry: "lib.Format(2 * time.Second)",
			want:  "2s",
		},
		{
			name:  "fmt_boxing",
			lib:   "import \"fmt\"\nimport \"time\"\n\nfunc Format(d time.Duration) string { return fmt.Sprint(d) }",
			entry: "lib.Format(2 * time.Second)",
			want:  "2s",
		},
		{
			name: "type_switch",
			lib: "import \"time\"\n\n" +
				"func kind(x any) string {\n\tswitch x.(type) {\n\tcase time.Duration:\n\t\treturn \"duration\"\n\tcase int64:\n\t\treturn \"int64\"\n\t}\n\treturn \"other\"\n}\n\n" +
				"func Kinds(d time.Duration, n int64) string { return kind(d) + \",\" + kind(n) }",
			entry: "lib.Kinds(2 * time.Second, 7)",
			want:  "duration,int64",
		},
		{
			name: "package_var",
			lib: "import \"hostprobe\"\nimport \"time\"\n\n" +
				"var Timeout = 3 * time.Second\n\nfunc TimeoutType() string { return hostprobe.TypeOf(&Timeout) }",
			entry: "lib.TimeoutType() + \" \" + lib.Timeout.String()",
			want:  "*time.Duration 3s",
		},
		{
			name: "make_slice",
			lib: "import \"hostprobe\"\nimport \"time\"\n\n" +
				"func Durations(n int) string {\n\tdurations := make([]time.Duration, n)\n\tdurations[0] = time.Second\n\treturn hostprobe.TypeOf(durations) + \" \" + hostprobe.Sum(durations).String()\n}",
			entry: "lib.Durations(3)",
			want:  "[]time.Duration 1s",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			modulePath := "example.com/named_" + tc.name
			libSources := map[string]map[string]string{
				"": {"lib.go": "package lib\n\n" + tc.lib + "\n"},
			}

			builder := app.NewService()
			builder.UseSymbolProviders(hostNamedTypeProvider{})
			descriptor := module.Descriptor{
				SchemaVersion: module.DescriptorVersion,
				Ref:           module.Ref{Path: modulePath, Version: "v0.0.0"},
			}
			bundle, err := builder.PackageModule(context.Background(),
				descriptor,
				modulePath,
				libSources,
				adapters.PackCompiledFileSetToBytes,
			)
			require.NoError(t, err)

			consumer := app.NewService()
			consumer.UseSymbolProviders(hostNamedTypeProvider{})
			reference := module.Ref{Path: modulePath, Version: "v0.0.0", Pin: bundle.Descriptor.Ref.Pin}
			_, err = consumer.LoadModule(context.Background(), bundle, reference, nil, adapters.LoadCompiledFromBytes)
			require.NoError(t, err)

			mainSources := map[string]map[string]string{
				"": {
					"main.go": "package main\n\nimport (\n\t\"time\"\n\n\t\"" + modulePath + "\"\n)\n\n" +
						"var _ = time.Second\n\nfunc entrypoint() string { return " + tc.entry + " }\n\nfunc main() {}\n",
				},
			}
			mainCfs, err := consumer.CompileProgram(context.Background(), "main", mainSources)
			require.NoError(t, err)

			result, err := consumer.ExecuteEntrypoint(context.Background(), mainCfs, "entrypoint")
			require.NoError(t, err)
			require.Equal(t, tc.want, fmt.Sprint(result))
		})
	}
}
