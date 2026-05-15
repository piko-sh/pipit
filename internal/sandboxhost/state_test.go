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

package sandboxhost

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"pipit.sh/pipit/internal/logging"
)

func TestIsolatedLoggerContextStaysSilent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if logging.LoggerFrom(isolatedLoggerContext(ctx, nil)).Enabled(ctx, slog.LevelInfo) {
		t.Fatal("audit logging is on for a host that configured no logger")
	}
	host := slog.New(slog.NewTextHandler(io.Discard, nil))
	if logging.LoggerFrom(isolatedLoggerContext(ctx, host)) != host {
		t.Fatal("the host's own logger did not survive the isolated context")
	}
}

func TestLaunchRecordLifecycle(t *testing.T) {
	t.Parallel()
	state := privateTestDirectory(t)
	record, err := createLaunchRecord(state, "tenant-a", false)
	if err != nil {
		t.Fatal(err)
	}
	if record.journal || record.journalDirectory() != "" || len(record.storeMetadata()) != 2 {
		t.Fatalf("worker record carries a journal: %+v", record)
	}
	for _, directory := range []string{record.checkpointDirectory(), record.approvalDirectory(), record.imagesDirectory()} {
		info, err := os.Stat(directory)
		if err != nil || !info.IsDir() || info.Mode().Perm() != launchRecordMode {
			t.Fatalf("record child %s: %v %v", directory, info, err)
		}
	}
	if _, err := os.Stat(filepath.Join(record.directory, launchRecordJournal)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("worker record created a journal directory:", err)
	}
	filesystem, err := createLaunchRecord(state, "tenant-a", true)
	if err != nil {
		t.Fatal(err)
	}
	if !filesystem.journal || filesystem.journalDirectory() == "" || len(filesystem.storeMetadata()) != 3 {
		t.Fatalf("filesystem record lacks its journal: %+v", filesystem)
	}
	other, err := createLaunchRecord(state, "tenant-b", false)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(other.directory) == filepath.Dir(record.directory) {
		t.Fatal("tenants share a record directory")
	}
	records, err := listLaunchRecords(state, "tenant-a")
	if err != nil || len(records) != 2 {
		t.Fatalf("tenant-a records: %v %v", records, err)
	}
	journals := 0
	for _, listed := range records {
		if listed.journal {
			journals++
		}
	}
	if journals != 1 {
		t.Fatal("listing lost the journal marker:", records)
	}
	if listed, err := listLaunchRecords(state, "tenant-c"); err != nil || len(listed) != 0 {
		t.Fatalf("unknown tenant: %v %v", listed, err)
	}
	if err := record.remove(); err != nil {
		t.Fatal(err)
	}
	if records, err := listLaunchRecords(state, "tenant-a"); err != nil || len(records) != 1 || !records[0].journal {
		t.Fatalf("removal left the wrong record: %v %v", records, err)
	}
	if tenantSlug("tenant-a") == tenantSlug("tenant-b") || len(tenantSlug("x")) != tenantSlugLength {
		t.Fatal("tenant slugs are not distinct or wrongly sized")
	}
}

func TestLaunchRecordsRequirePrivateState(t *testing.T) {
	t.Parallel()
	shared := t.TempDir()
	if err := os.Chmod(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := createLaunchRecord(shared, "tenant", false); !errors.Is(err, ErrInvalidIsolatedConfig) {
		t.Fatal("shared state directory accepted:", err)
	}
	if _, err := listLaunchRecords(shared, "tenant"); !errors.Is(err, ErrInvalidIsolatedConfig) {
		t.Fatal("shared state directory listed:", err)
	}
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := createLaunchRecord(missing, "tenant", false); !errors.Is(err, ErrInvalidIsolatedConfig) {
		t.Fatal("missing state directory accepted:", err)
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := createLaunchRecord(file, "tenant", false); !errors.Is(err, ErrInvalidIsolatedConfig) {
		t.Fatal("regular file accepted as state directory:", err)
	}
	if err := launchRecordError(nil); err != nil {
		t.Fatal(err)
	}
	if err := launchRecordError(os.ErrPermission); !errors.Is(err, ErrIsolatedUnavailable) {
		t.Fatal("record failure not mapped to unavailability:", err)
	}
}

func TestLaunchRecordsAreBounded(t *testing.T) {
	t.Parallel()
	state := privateTestDirectory(t)
	tenant := filepath.Join(state, tenantSlug("tenant"))
	if err := os.Mkdir(tenant, launchRecordMode); err != nil {
		t.Fatal(err)
	}
	for index := range maximumLaunchRecords + 1 {
		if err := os.Mkdir(filepath.Join(tenant, "record-"+strconv.Itoa(index)), launchRecordMode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(tenant, "stray"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := listLaunchRecords(state, "tenant"); !errors.Is(err, errLaunchRecords) {
		t.Fatal("record bound not enforced:", err)
	}
	if err := os.Remove(filepath.Join(tenant, "record-0")); err != nil {
		t.Fatal(err)
	}
	records, err := listLaunchRecords(state, "tenant")
	if err != nil || len(records) != maximumLaunchRecords {
		t.Fatalf("bounded listing: %d %v", len(records), err)
	}
	for index := 1; index < len(records); index++ {
		if records[index-1].directory >= records[index].directory {
			t.Fatal("records are not sorted")
		}
	}
}

func privateTestDirectory(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, launchRecordMode); err != nil {
		t.Fatal(err)
	}
	return directory
}
