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

package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func handlerNamesByTier() [4][256]string {
	var out [4][256]string
	for op, handler := range handlerRegistrations {
		out[isa.TierMain][uint8(op)] = functionName(handler)
	}
	for op, handler := range flatTier1Registrations {
		out[isa.TierSub1][uint8(op)] = functionName(handler)
	}
	for op, handler := range flatTier2Registrations {
		out[isa.TierSub2][uint8(op)] = functionName(handler)
	}
	for op, handler := range flatTier3Registrations {
		out[isa.TierSub3][uint8(op)] = functionName(handler)
	}
	return out
}

func functionName(handler opcodeHandler) string {
	full := runtime.FuncForPC(reflect.ValueOf(handler).Pointer()).Name()
	return full[strings.LastIndex(full, ".")+1:]
}

func directExitsByOpcode(t *testing.T) (map[isa.Opcode][2]string, map[string]string) {
	t.Helper()
	source, err := os.ReadFile("dispatch_direct_exits_generated.go")
	require.NoError(t, err)
	registry, err := os.ReadFile("direct_exit_registry_generated.go")
	require.NoError(t, err)
	reasons := map[string]string{}
	for _, m := range regexp.MustCompile(`\{Name: "(\w+)", ExitReason: (\w+)\}`).FindAllStringSubmatch(string(registry), -1) {
		reasons[m[1]] = m[2]
	}
	out := map[isa.Opcode][2]string{}
	opByName := opcodeConstants()
	for _, m := range regexp.MustCompile(`asmJumpTable\[isa\.(Op\w+)\] = reflect\.ValueOf\((\w+)\)\.Pointer\(\)`).FindAllStringSubmatch(string(source), -1) {
		op, ok := opByName[m[1]]
		require.Truef(t, ok, "direct exit names unknown opcode %s", m[1])
		out[op] = [2]string{m[2], reasons[m[2]]}
	}
	return out, reasons
}

func opcodeConstants() map[string]isa.Opcode {
	names := enumConstantNamesFromSource()
	out := map[string]isa.Opcode{}
	for code, name := range names[isa.TierMain] {
		if name != "" {
			out[name] = isa.Opcode(code)
		}
	}
	return out
}

func enumConstantNamesFromSource() [4][256]string {
	var out [4][256]string
	source, err := os.ReadFile(filepath.Join("..", "isa", "opcode.go"))
	if err != nil {
		panic(err)
	}
	starts := map[string]isa.Tier{" Opcode = iota": isa.TierMain, " SubOpcode = iota": isa.TierSub1, " SubOpcodeTier2 = iota": isa.TierSub2, " SubOpcodeTier3 = iota": isa.TierSub3}
	member := regexp.MustCompile(`^\t([A-Za-z]\w*)\b`)
	inBlock, tier, index := false, isa.TierMain, 0
	for line := range strings.SplitSeq(string(source), "\n") {
		if !inBlock {
			for suffix, candidate := range starts {
				if strings.HasSuffix(strings.TrimSpace(line), suffix) {
					inBlock, tier, index = true, candidate, 0
					out[tier][0] = strings.Fields(line)[0]
					index = 1
				}
			}
			continue
		}
		if line == ")" {
			inBlock = false
			continue
		}
		m := member.FindStringSubmatch(line)
		if m == nil || strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		if index < 256 {
			out[tier][index] = m[1]
		}
		index++
	}
	return out
}
