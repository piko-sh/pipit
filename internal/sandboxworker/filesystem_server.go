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

package sandboxworker

import (
	"slices"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/sandboxbroker"
	"pipit.sh/pipit/internal/sandboxwire"
)

// FilesystemProfile selects one source submission with fixed filesystem RPC stubs.
const FilesystemProfile = "restricted-filesystem-v1"

// FilesystemConfiguration carries opaque authority only, never host paths or handles. The
// native broker independently binds these names to host-approved pinned roots.
type FilesystemConfiguration struct {
	// Limits selects finite cumulative quotas for the broker budget.
	Limits *sandboxbroker.FilesystemLimits `json:"limits"`

	// Profile names the reviewed filesystem worker protocol.
	Profile string `json:"profile"`

	// Imports lists the host-approved package paths for the submission.
	Imports []string `json:"imports"`

	// Roots lists named authority grants for broker filesystem access.
	Roots []sandboxbroker.RootGrant `json:"roots"`
}

// serveFilesystem installs the reviewed manifest after exact policy validation.
//
// Takes stream (Transport) which carries the confined channel.
// Takes message (sandboxwire.Message) which holds the immutable configuration.
//
// Returns error for invalid policy, protocol or source execution framing.
func (session *connection) serveFilesystem(stream transport, message sandboxwire.Message) error {
	var config FilesystemConfiguration
	if err := message.DecodePayload(&config); err != nil {
		return err
	}
	if err := validateFilesystemSubmission(config, Request{Kind: "expression", Source: "", Entrypoint: ""}); err != nil {
		return err
	}
	proxy, err := sandboxbroker.NewFilesystemProxy(session.codec, session.machine, config.Roots, *config.Limits)
	if err != nil {
		return err
	}
	var restricted app.RestrictedConfig
	restricted.Imports = config.Imports
	interpreter, err := session.newFilesystemInterpreter(restricted, proxy)
	if err != nil {
		return err
	}
	if err := session.send(sandboxwire.Ready, 0, struct{}{}); err != nil {
		return err
	}
	message, err = session.receive()
	if err != nil {
		return err
	}
	if message.Kind == sandboxwire.Close {
		return message.DecodePayload(&struct{}{})
	}
	return session.serveSubmission(stream, interpreter, message)
}

// newFilesystemInterpreter builds the filesystem interpreter over the injected provider
// when one is linked, otherwise over the math-only surface.
//
// Takes restricted (app.RestrictedConfig) which selects imports.
// Takes proxy (*sandboxbroker.FilesystemProxy) which provides filesystem RPC stubs.
//
// Returns *app.RestrictedInterpreter with filesystem support.
// Returns error for a configuration failure.
func (session *connection) newFilesystemInterpreter(restricted app.RestrictedConfig, proxy *sandboxbroker.FilesystemProxy) (*app.RestrictedInterpreter, error) {
	if session.provider != nil {
		return app.NewFilesystemRestrictedInterpreterWithProvider(restricted, proxy, session.provider)
	}
	return app.NewFilesystemRestrictedInterpreter(restricted, proxy)
}

// validateFilesystemSubmission checks source semantics without granting host I/O.
//
// Takes config (FilesystemConfiguration) which holds the immutable host-approved
// settings.
// Takes request (Request) which contains the source submission.
//
// Returns error for missing authority metadata or unreviewed imports.
func validateFilesystemSubmission(config FilesystemConfiguration, request Request) error {
	if config.Profile != FilesystemProfile || config.Roots == nil || config.Limits == nil ||
		!slices.Contains(config.Imports, "pipit/fs") {
		return sandboxwire.ErrProtocol
	}
	budget, err := sandboxbroker.NewFilesystemBudget(config.Roots, *config.Limits)
	if err != nil {
		return err
	}
	budget.Close()
	imports := slices.DeleteFunc(slices.Clone(config.Imports), func(path string) bool { return path == "pipit/fs" })
	return validateSubmission(Configuration{Profile: Profile, Imports: imports}, request)
}
