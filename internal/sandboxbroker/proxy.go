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
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"

	"pipit.sh/pipit/internal/sandboxwire"
)

// FilesystemProxy exposes only fixed, bounded RPC methods inside a confined worker. The
// host must independently authorise every call and enforce hard execution deadlines.
type FilesystemProxy struct {
	// codec is the framed channel shared with the host.
	codec *sandboxwire.Codec

	// machine tracks the protocol lifecycle state.
	machine *sandboxwire.Machine

	// budget tracks cumulative reservation quotas.
	budget *FilesystemBudget

	// failure holds the first latched terminal error.
	failure error

	// nextID is the monotonic call identifier for the next request.
	nextID uint64

	// mutex guards the latched failure state.
	mutex sync.Mutex

	// active is true while an operation holds exclusive access.
	active atomic.Bool
}

// NewFilesystemProxy shares the worker's framed channel and lifecycle ledger. Only
// execution may use the proxy; compilation and handshake must not invoke it.
//
// Takes codec (*sandboxwire.Codec) which is the worker's framed channel.
// Takes machine (*sandboxwire.Machine) which tracks the protocol lifecycle state.
// Takes grants ([]RootGrant) which are the host-selected opaque root authorities.
// Takes limits (FilesystemLimits) which are the host-selected cumulative quotas.
//
// Returns a worker-local proxy, never host handles or privileged callbacks.
func NewFilesystemProxy(codec *sandboxwire.Codec, machine *sandboxwire.Machine, grants []RootGrant, limits FilesystemLimits) (*FilesystemProxy, error) {
	if codec == nil || machine == nil {
		return nil, sandboxwire.ErrProtocol
	}
	budget, err := NewFilesystemBudget(grants, limits)
	if err != nil {
		return nil, err
	}
	return &FilesystemProxy{
		codec: codec, machine: machine, budget: budget, failure: nil, nextID: 1,
		mutex: sync.Mutex{}, active: atomic.Bool{},
	}, nil
}

// Read requests a bounded prefix from a regular file beneath an opaque root.
//
// Takes root (string) which is the opaque root identifier from the host grant.
// Takes path (string) which is the portable relative path beneath the root.
// Takes maximum (int) which is the maximum byte count to read.
//
// Returns copied bytes or an error without partial data.
func (proxy *FilesystemProxy) Read(root, path string, maximum int) ([]byte, error) {
	response, err := proxy.invoke(ReadFile, root, path, nil, maximum)
	return response.Data, err
}

// Write requests staged replacement of one file beneath an opaque writable root.
//
// Takes root (string) which is the opaque root identifier from the host grant.
// Takes path (string) which is the portable relative path beneath the root.
// Takes data ([]byte) which is the bounded write payload.
//
// Returns an error on failure; errors never imply that publication was rolled back.
func (proxy *FilesystemProxy) Write(root, path string, data []byte) error {
	_, err := proxy.invoke(WriteFile, root, path, data, len(data))
	return err
}

// List requests bounded portable entry names without exposing native directory objects.
//
// Takes root (string) which is the opaque root identifier.
// Takes path (string) which is the relative path beneath the root.
// Takes maximum (int) which caps the entry count.
//
// Returns names, the count of entries omitted because the protocol cannot carry them, and
// an error.
func (proxy *FilesystemProxy) List(root, path string, maximum int) ([]string, int, error) {
	response, err := proxy.invoke(ListDirectory, root, path, nil, maximum)
	return response.Entries, response.Skipped, err
}

// Err reports the first terminal failure even if interpreted code ignored an error.
//
// Returns the latched failure or nil.
//
// Safe for concurrent use by multiple goroutines.
func (proxy *FilesystemProxy) Err() error {
	if proxy == nil {
		return ErrClosed
	}
	proxy.mutex.Lock()
	defer proxy.mutex.Unlock()
	return proxy.failure
}

// invoke validates arguments before encoding and serialises access without queuing.
//
// Takes operation (Operation) which identifies the filesystem action type.
// Takes root (string) which is the opaque root identifier.
// Takes path (string) which is the portable relative path beneath the root.
// Takes data ([]byte) which is the bounded write payload, or nil for reads and lists.
// Takes maximum (int) which is the reserved byte or entry count.
//
// Returns independently validated reply data or a latched terminal error.
func (proxy *FilesystemProxy) invoke(operation Operation, root, path string, data []byte, maximum int) (response FilesystemResponse, err error) {
	if proxy == nil {
		return FilesystemResponse{}, ErrClosed
	}
	if !proxy.active.CompareAndSwap(false, true) {
		proxy.fail(errBusy)
		return FilesystemResponse{}, errBusy
	}
	defer proxy.active.Store(false)
	defer func() {
		if err != nil {
			proxy.fail(err)
		}
		if failure := proxy.Err(); failure != nil {
			err = failure
			response = FilesystemResponse{Code: "", Data: nil, Entries: nil, Written: 0, Skipped: 0}
		}
	}()
	if err := proxy.Err(); err != nil {
		return FilesystemResponse{}, err
	}
	if !validRootName(root) || !validRelativePath(path, operation == ListDirectory) ||
		maximum < 0 || maximum > maximumFileBytes || operation == ListDirectory && maximum > maximumListEntries {
		return FilesystemResponse{}, sandboxwire.ErrProtocol
	}
	request := filesystemWireRequest{Operation: operation, Root: root, Path: path, Data: nil, MaxBytes: nil, MaxEntries: nil}
	switch operation {
	case ReadFile:
		request.MaxBytes, err = json.Marshal(maximum)
	case WriteFile:
		if data == nil {
			data = []byte{}
		}
		request.Data, err = json.Marshal(data)
	case ListDirectory:
		request.MaxEntries, err = json.Marshal(maximum)
	}
	if err != nil {
		return FilesystemResponse{}, err
	}
	return proxy.exchange(request)
}

// exchange sends one authorised Call and accepts only its correlated Reply.
//
// Takes request (filesystemWireRequest) which is a fixed request schema whose unused
// fields are omitted during encoding.
//
// Returns validated output after releasing the local reservation.
func (proxy *FilesystemProxy) exchange(request filesystemWireRequest) (response FilesystemResponse, err error) {
	fields := map[string]any{"operation": request.Operation, "root": request.Root, "path": request.Path}
	switch request.Operation {
	case ReadFile:
		fields["max_bytes"] = request.MaxBytes
	case WriteFile:
		fields["data"] = request.Data
	case ListDirectory:
		fields["max_entries"] = request.MaxEntries
	}
	payload, err := json.Marshal(fields)
	if err != nil {
		return FilesystemResponse{}, err
	}
	message := sandboxwire.Message{Kind: sandboxwire.Call, ID: proxy.nextID, Payload: payload}
	call, err := proxy.budget.Admit(message)
	if err != nil {
		return FilesystemResponse{}, err
	}
	used := 0
	defer func() { err = errors.Join(err, call.Finish(used)) }()
	if err := proxy.machine.Observe(sandboxwire.WorkerToHost, message); err != nil {
		return FilesystemResponse{}, err
	}
	if err := proxy.codec.Write(message); err != nil {
		return FilesystemResponse{}, err
	}
	reply, err := proxy.codec.Read()
	if err != nil {
		return FilesystemResponse{}, err
	}
	if err := proxy.machine.Observe(sandboxwire.HostToWorker, reply); err != nil {
		return FilesystemResponse{}, err
	}
	if reply.Kind != sandboxwire.Reply {
		return FilesystemResponse{}, sandboxwire.ErrProtocol
	}
	reply.Kind = sandboxwire.Result
	response, err = decodeFilesystemResponse(reply, proxy.nextID, call)
	if err != nil {
		return FilesystemResponse{}, err
	}
	if response.Code != "" {
		return FilesystemResponse{}, ErrDenied
	}
	used = len(response.Data) + len(response.Entries) + response.Skipped + response.Written
	proxy.nextID++
	return response, nil
}

// fail permanently latches the first failure and prevents further reservations.
//
// Takes err (error) which is the operation failure, never an interpreted callback.
//
// Safe for concurrent use by multiple goroutines.
func (proxy *FilesystemProxy) fail(err error) {
	proxy.mutex.Lock()
	defer proxy.mutex.Unlock()
	if proxy.failure == nil {
		proxy.failure = err
	}
	proxy.budget.Close()
}
