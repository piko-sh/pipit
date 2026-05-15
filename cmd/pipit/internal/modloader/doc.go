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

// Package modloader is pipit's CLI-side module-loading layer. It implements the
// pipit-flavoured policy on top of the generic module-loading contract
// (pipit.sh/pipit/module).
//
//   - Lockfile (script.lock) for deterministic, frozen-mode reruns and approval
//     persistence.
//   - Interactive [CapabilityHook] that prompts the operator on a TTY when an unapproved
//     capability claim is hit and persists decisions to the lockfile.
//   - Layered [ModuleProvider]: on-disk artefact cache then GOPROXY. See [CacheMode] and
//     [ResolveCacheSpec].
//   - CLI verbs (get/list/inspect/verify) backing the "pipit module ..." subcommand.
package modloader
