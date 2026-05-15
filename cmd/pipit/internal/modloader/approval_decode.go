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

package modloader

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

const (
	// maximumApprovalModules is the ceiling on module entries retained per approval
	// document.
	maximumApprovalModules = 1024

	// maximumModuleApprovals is the ceiling on capability strings retained per module entry.
	maximumModuleApprovals = 256
)

// decodeApprovalFile accepts the exact approval schema without ambiguous JSON fields.
//
// Takes data ([]byte) which is one bounded approval document.
//
// Returns Lockfile which is the complete decoded content.
// Returns error which reports syntax, schema or collection-limit failure.
func decodeApprovalFile(data []byte) (Lockfile, error) {
	var result Lockfile
	if len(data) == 0 || len(data) > maximumApprovalFileBytes || !utf8.Valid(data) {
		return result, errors.New("invalid approval document size or UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	err := decodeApprovalObject(decoder, func(name string) error {
		switch name {
		case "generated_at":
			return decoder.Decode(&result.GeneratedAt)
		case "script":
			return decodeApprovalText(decoder, &result.Script)
		case "script_hash":
			return decodeApprovalText(decoder, &result.ScriptHash)
		case "schema":
			return decoder.Decode(&result.Schema)
		case "modules":
			return decodeApprovalModules(decoder, &result.Modules)
		default:
			return fmt.Errorf("unknown approval field %q", name)
		}
	})
	if err == nil {
		if _, trailing := decoder.Token(); trailing != io.EOF {
			err = errors.New("trailing approval document data")
		}
	}
	if err != nil {
		return Lockfile{}, err
	}
	return result, nil
}

// decodeApprovalObject refuses duplicate members before assigning their values.
//
// Takes decoder (*json.Decoder) which reads the JSON stream.
// Takes field (func(string) error) which handles each exact JSON field name.
//
// Returns error for non-objects, repeated members or rejected fields.
func decodeApprovalObject(decoder *json.Decoder, field func(string) error) error {
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return errors.New("expected approval object")
	}
	seen := make(map[string]bool)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		name, valid := token.(string)
		if !valid || seen[name] {
			return errors.New("duplicate or invalid approval field")
		}
		seen[name] = true
		if err := field(name); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

// decodeApprovalModules bounds module entries and rejects conflicting identities.
//
// Takes decoder (*json.Decoder) which reads the JSON stream.
// Takes modules (*[]LockedModule) which receives the decoded entries.
//
// Returns error before retaining entries beyond the module ceiling.
func decodeApprovalModules(decoder *json.Decoder, modules *[]LockedModule) error {
	opening, err := decoder.Token()
	if err != nil {
		return err
	}
	if opening == nil {
		return nil
	}
	if opening != json.Delim('[') {
		return errors.New("expected approval module array")
	}
	seen := make(map[[2]string]bool)
	for decoder.More() {
		if len(*modules) >= maximumApprovalModules {
			return errors.New("approval module count exceeded")
		}
		var module LockedModule
		if err := decodeApprovalModule(decoder, &module); err != nil {
			return err
		}
		identity := [2]string{module.Path, module.Version}
		if seen[identity] {
			return errors.New("duplicate approval module identity")
		}
		seen[identity] = true
		*modules = append(*modules, module)
	}
	_, err = decoder.Token()
	return err
}

// decodeApprovalModule decodes the exact fields of one module approval.
//
// Takes decoder (*json.Decoder) which reads the JSON stream.
// Takes module (*LockedModule) which receives the decoded fields.
//
// Returns error for ambiguous fields or malformed values.
func decodeApprovalModule(decoder *json.Decoder, module *LockedModule) error {
	return decodeApprovalObject(decoder, func(name string) error {
		switch name {
		case "approved_at":
			return decoder.Decode(&module.ApprovedAt)
		case "path":
			return decodeApprovalText(decoder, &module.Path)
		case "version":
			return decodeApprovalText(decoder, &module.Version)
		case "bundle_digest":
			return decodeApprovalText(decoder, &module.BundleDigest)
		case "approved_via":
			return decodeApprovalText(decoder, &module.ApprovedVia)
		case "approved_capabilities":
			return decodeApprovalCapabilities(decoder, &module.ApprovedCapabilities)
		default:
			return fmt.Errorf("unknown module approval field %q", name)
		}
	})
}

// decodeApprovalCapabilities bounds retained capability strings.
//
// Takes decoder (*json.Decoder) which reads the JSON stream.
// Takes capabilities (*[]string) which receives the decoded entries.
//
// Returns error before retaining entries beyond the per-module ceiling.
func decodeApprovalCapabilities(decoder *json.Decoder, capabilities *[]string) error {
	opening, err := decoder.Token()
	if err != nil {
		return err
	}
	if opening == nil {
		return nil
	}
	if opening != json.Delim('[') {
		return errors.New("expected capability array")
	}
	for decoder.More() {
		if len(*capabilities) >= maximumModuleApprovals {
			return errors.New("approval capability count exceeded")
		}
		var capability string
		if err := decodeApprovalText(decoder, &capability); err != nil {
			return err
		}
		*capabilities = append(*capabilities, capability)
	}
	_, err = decoder.Token()
	return err
}

// decodeApprovalText rejects null and non-string values instead of coercing them.
//
// Takes decoder (*json.Decoder) which supplies the next JSON scalar.
// Takes destination (*string) which receives the decoded value.
//
// Returns error without assigning a malformed value.
func decodeApprovalText(decoder *json.Decoder, destination *string) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	value, valid := token.(string)
	if !valid {
		return errors.New("expected approval string")
	}
	*destination = value
	return nil
}
