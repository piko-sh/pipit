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

// ChargeOutput accounts for validated script output in the native diagnostic budget. It
// remains effective after normal exit when a buffered result may still be read.
//
// Takes count (int) which is the decoded script-output byte count, not a worker claim.
//
// Returns error on invalid accounting or combined output overflow.
//
// Safe for concurrent use by multiple goroutines.
func (process *WorkerProcess) ChargeOutput(count int) error {
	if count < 0 || process.output == nil {
		return ErrInvalidLimits
	}
	output := process.output
	output.mutex.Lock()
	defer output.mutex.Unlock()
	if output.exceeded {
		return ErrWorkerOutput
	}
	if count > output.limit-output.used {
		output.exceeded = true
		output.cancel()
		return ErrWorkerOutput
	}
	output.used += count
	return nil
}

// failure reports output overflow, including accounting performed after process exit.
//
// Returns error when either native or script output exceeded the shared budget.
//
// Safe for concurrent use by multiple goroutines.
func (output *workerOutput) failure() error {
	output.mutex.Lock()
	defer output.mutex.Unlock()
	if output.exceeded {
		return ErrWorkerOutput
	}
	return nil
}
