//go:build linux && (amd64 || arm64)

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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math"
	"os"

	"golang.org/x/sys/unix"

	"pipit.sh/pipit/internal/safeconv"
	"pipit.sh/pipit/internal/sandboxbroker"
	"pipit.sh/pipit/internal/sandboxwire"
)

// maximumServiceRecoveryIdentityBytes is the size limit for encoded recovery identity
// metadata.
const maximumServiceRecoveryIdentityBytes = 1024

// verifyServiceRecoveryHost checks that the independently approved original host has
// exited. Current binding must be independently derived in the same boot and PID
// namespace.
//
// Takes checkpoint ([]byte) which is the stored checkpoint.
// Takes approved ([sha256.Size]byte) which is the independently retained approval.
// Takes binding ([sha256.Size]byte) which is the independently derived binding.
//
// Returns ErrServiceBusy for a live original host, or failure on ambiguous observations.
func verifyServiceRecoveryHost(ctx context.Context, checkpoint []byte, approved, binding [sha256.Size]byte) error {
	if ctx == nil {
		return ErrInvalidLimits
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	metadata, err := sandboxbroker.RecoveryCheckpointContext(checkpoint, approved, binding)
	if err != nil {
		return err
	}
	identity, err := decodeServiceRecoveryIdentity(metadata)
	if err != nil {
		return err
	}
	if err := verifyRecoveryHost(identity); err != nil {
		return err
	}
	return ctx.Err()
}

// decodeServiceRecoveryIdentity accepts only canonical bounded native-owner metadata.
//
// Takes encoded ([]byte) which holds the independently approved context bytes, never
// worker-provided process claims.
//
// Returns complete original parent, service and process identities without opening paths.
func decodeServiceRecoveryIdentity(encoded []byte) (serviceRecoveryIdentity, error) {
	var empty serviceRecoveryIdentity
	if len(encoded) == 0 || len(encoded) > maximumServiceRecoveryIdentityBytes {
		return empty, ErrInvalidLimits
	}
	var identity serviceRecoveryIdentity
	message := sandboxwire.Message{Kind: sandboxwire.Configure, ID: 0, Payload: encoded}
	if err := message.DecodePayload(&identity); err != nil {
		return empty, err
	}
	canonical, err := json.Marshal(identity)
	if err != nil || !bytes.Equal(canonical, encoded) {
		return empty, errors.Join(ErrInvalidLimits, err)
	}
	if !validServiceRecoveryProfile(identity) || !isServiceGroupName(identity.Name) || identity.PID <= 0 || identity.PID > math.MaxInt32 ||
		identity.Host[1] == 0 || identity.Parent.Inode == 0 || identity.Parent.Mount == 0 ||
		identity.Group.Inode == 0 || identity.Group.Mount == 0 || identity.Parent == identity.Group {
		return empty, ErrInvalidLimits
	}
	return identity, nil
}

// validServiceRecoveryProfile prevents omission or injection of image-store ownership.
//
// Takes identity (serviceRecoveryIdentity) which holds the decoded profile and images.
//
// Returns true only for the exact authority shape required by each recognised profile.
func validServiceRecoveryProfile(identity serviceRecoveryIdentity) bool {
	var empty sandboxbroker.LinuxImageStoreIdentity
	switch identity.Profile {
	case serviceRecoveryProfile:
		return identity.Images == empty
	case serviceRecoveryImageProfile:
		return identity.Images.Inode != 0 && identity.Images.MountID != 0
	default:
		return false
	}
}

// verifyRecoveryHost observes the approved numeric PID through a fresh stable pidfd.
//
// Takes identity (serviceRecoveryIdentity) which is the validated original identity under
// an independently verified host binding.
//
// Returns success only for an absent PID or readiness of the matching kernel process.
func verifyRecoveryHost(identity serviceRecoveryIdentity) (result error) {
	descriptor, err := unix.PidfdOpen(identity.PID, 0)
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	if err != nil {
		return err
	}
	host := os.NewFile(uintptr(descriptor), "recovery-original-host")
	defer func() { result = errors.Join(result, host.Close()) }()
	if err := validateWatchdogHost(host); err != nil {
		return err
	}
	current, err := watchdogFileIdentity(host)
	if err != nil {
		return err
	}
	if current != identity.Host {
		return ErrUnavailable
	}
	return recoveryHostReady(host)
}

// recoveryHostReady performs one non-blocking observation without signalling the host.
//
// Takes host (*os.File) which is the validated pidfd whose identity matches independently
// approved metadata.
//
// Returns busy for a live process and refuses unexpected poll events or errors.
func recoveryHostReady(host *os.File) error {
	if host.Fd() > math.MaxInt32 {
		return ErrUnavailable
	}
	events := []unix.PollFd{{Fd: safeconv.IntToInt32(int(host.Fd())), Events: unix.POLLIN, Revents: 0}}
	count, err := unix.Ppoll(events, &unix.Timespec{Sec: 0, Nsec: 0}, nil)
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrServiceBusy
	}
	if count != 1 || events[0].Revents == 0 || events[0].Revents & ^int16(unix.POLLIN|unix.POLLHUP) != 0 {
		return ErrUnavailable
	}
	return nil
}
