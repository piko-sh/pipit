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

package sandboxlinux

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/sys/unix"
	"pipit.sh/pipit/internal/fault"
)

const (
	// serviceGroupPrefix starts every service cgroup name; the tenant digest follows.
	serviceGroupPrefix = "pipit-service-"

	// serviceGroupDigestLength is the number of hexadecimal digest characters in a name.
	serviceGroupDigestLength = 24

	// maximumTenantBytes bounds a host-selected tenant identity.
	maximumTenantBytes = 256
)

var (
	// errInvalidTenant reports a tenant identity that is empty, too long, not UTF-8 or
	// contains a NUL byte.
	errInvalidTenant = fmt.Errorf("%w: linux isolated tenant identity is invalid", ErrInvalidLimits)

	// ErrServiceBusy reports an existing service reservation, including abandoned ones.
	ErrServiceBusy = errors.New("linux isolated service admission is occupied")
)

// validateTenant checks a host-selected tenant identity.
//
// Takes tenant (string) which the host chose; never a value supplied by a script.
//
// Returns error which is ErrInvalidTenant when the identity is unusable.
func validateTenant(tenant string) error {
	if len(tenant) == 0 || len(tenant) > maximumTenantBytes || !utf8.ValidString(tenant) || strings.IndexByte(tenant, 0) >= 0 {
		return errInvalidTenant
	}
	return nil
}

// serviceGroupNameFor derives the service cgroup name of a tenant: the prefix followed by
// the first characters of the tenant's SHA-256 digest, so any tenant string yields a
// short valid directory name and two tenants never share a reservation.
//
// Takes tenant (string) which has passed ValidateTenant.
//
// Returns string which is the cgroup directory name under the delegated parent.
func serviceGroupNameFor(tenant string) string {
	digest := sha256.Sum256([]byte(tenant))
	return serviceGroupPrefix + hex.EncodeToString(digest[:])[:serviceGroupDigestLength]
}

// isServiceGroupName reports whether name has the shape serviceGroupNameFor produces.
//
// Takes name (string) which is a cgroup directory name.
//
// Returns bool which is true for the prefix followed by exactly the digest characters.
func isServiceGroupName(name string) bool {
	if !strings.HasPrefix(name, serviceGroupPrefix) || len(name) != len(serviceGroupPrefix)+serviceGroupDigestLength {
		return false
	}
	for _, r := range name[len(serviceGroupPrefix):] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// newServiceGroup reserves one aggregate slot per tenant across hosts sharing a delegated
// parent.
//
// Takes parent (string) which is the trusted delegated parent path.
// Takes limits (Limits) which holds the resource settings before preparing worker images.
// Takes tenant (string) which selects the reservation; two tenants never contend.
//
// Returns a new bounded owner. Existing reservations are never adopted or removed.
func newServiceGroup(parent string, limits Limits, tenant string) (*Group, error) {
	if err := validateTenant(tenant); err != nil {
		return nil, err
	}
	group, err := newRootGroup(parent, limits, serviceGroupNameFor(tenant))
	if errors.Is(err, unix.EEXIST) {
		return nil, fmt.Errorf(fault.ErrChainFmt, ErrServiceBusy, err)
	}
	return group, err
}
