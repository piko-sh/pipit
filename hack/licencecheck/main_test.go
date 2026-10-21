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

package main

import (
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/hack/licencecheck/checker"
)

const runMainEnvironment = "LICENCECHECK_RUN_MAIN"

func TestMainDescribesItsFlagsToGoVet(t *testing.T) {
	if os.Getenv(runMainEnvironment) != "" {
		os.Args = []string{"licencecheck", "-flags"}
		main()
		return
	}
	arguments := []string{"-test.run=^TestMainDescribesItsFlagsToGoVet$"}
	if coverDirectory := flag.Lookup("test.gocoverdir"); coverDirectory != nil && coverDirectory.Value.String() != "" {
		arguments = append(arguments, "-test.gocoverdir="+coverDirectory.Value.String())
	}
	command := exec.CommandContext(t.Context(), os.Args[0], arguments...)
	command.Env = append(os.Environ(), runMainEnvironment+"=1")
	output, err := command.Output()
	require.NoError(t, err)

	var described []struct{ Name string }
	require.NoError(t, json.Unmarshal(output, &described), "go vet reads the flag description as JSON: %s", output)
	names := make([]string, 0, len(described))
	for _, entry := range described {
		names = append(names, entry.Name)
	}
	for _, analyser := range checker.Analysers() {
		require.Contains(t, names, analyser.Name, "each analyser can be switched on its own")
	}
}
