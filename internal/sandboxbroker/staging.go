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

package sandboxbroker

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
)

const (
	// stagingPrefix is the fixed prefix for private publication link names.
	stagingPrefix = ".pipit-stage-"

	// stagingNamespaceBytes is the byte length of a random staging namespace.
	stagingNamespaceBytes = 16
)

// StagingName identifies one operation's private publication link. The namespace must
// come from trusted sealed bootstrap, never script input.
//
// Takes namespace (string) which is the canonical staging namespace.
// Takes identity (uint64) which is the non-zero broker operation identity.
//
// Returns one bounded basename, or an invalid-policy error.
func StagingName(namespace string, identity uint64) (string, error) {
	if !validStagingNamespace(namespace) || identity == 0 {
		return "", ErrInvalidPolicy
	}
	return stagingPrefix + namespace + "-" + strconv.FormatUint(identity, 10), nil
}

// newStagingNamespace generates a private 128-bit host-selected publication identity.
//
// Returns a canonical namespace or an entropy-source error before any I/O grant.
func newStagingNamespace() (string, error) {
	var entropy [stagingNamespaceBytes]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(entropy[:]), nil
}

// validStagingNamespace requires an exact lower-case hexadecimal namespace.
//
// Takes namespace (string) which is the namespace decoded from immutable host policy.
//
// Returns false for non-canonical or path-like input.
func validStagingNamespace(namespace string) bool {
	if len(namespace) != 2*stagingNamespaceBytes {
		return false
	}
	for _, character := range namespace {
		if character >= '0' && character <= '9' || character >= 'a' && character <= 'f' {
			continue
		}
		return false
	}
	return true
}
