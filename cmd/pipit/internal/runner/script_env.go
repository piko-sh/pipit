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

package runner

import (
	"os"
	"path/filepath"
	"strconv"
)

const (
	// ScriptDirectoryEnv names the variable holding the script's absolute directory.
	ScriptDirectoryEnv = "PIPIT_SCRIPT_DIR"

	// ArgumentCountEnv names the variable holding the number of positional script arguments.
	ArgumentCountEnv = "PIPIT_ARGC"

	// ArgumentEnvPrefix starts the name of the variable holding each positional script
	// argument, followed by its index.
	ArgumentEnvPrefix = "PIPIT_ARG_"

	// scriptEnvFixedEntries is the number of variables ScriptEnv sets besides the arguments.
	scriptEnvFixedEntries = 2
)

// ScriptEnv builds the variables the CLI overlays on a script's environment: the
// positional arguments as PIPIT_ARGC and PIPIT_ARG_<n> (the same arguments reach os.Args
// through WithArgs) and the script's directory as PIPIT_SCRIPT_DIR.
//
// Takes target (string) which is the file or directory being run.
// Takes scriptArguments ([]string) which holds the positional arguments.
//
// Returns map[string]string which is the environment overlay.
func ScriptEnv(target string, scriptArguments []string) map[string]string {
	env := make(map[string]string, len(scriptArguments)+scriptEnvFixedEntries)
	env[ArgumentCountEnv] = strconv.Itoa(len(scriptArguments))
	for index, value := range scriptArguments {
		env[ArgumentEnvPrefix+strconv.Itoa(index)] = value
	}
	env[ScriptDirectoryEnv] = scriptDirectory(target)
	return env
}

// scriptDirectory returns the target directory or a file target's parent. If the
// absolute path cannot be resolved, it returns the cleaned relative path.
//
// Takes target (string) which is the file or directory being run.
//
// Returns string which is the directory.
func scriptDirectory(target string) string {
	directory := target
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		directory = filepath.Dir(target)
	}
	if absolute, err := filepath.Abs(directory); err == nil {
		return absolute
	}
	return filepath.Clean(directory)
}
