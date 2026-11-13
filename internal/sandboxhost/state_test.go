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
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/logging"
)

func TestIsolatedLoggerContextStaysSilent(t *testing.T) {
	t.Parallel()
	ctx, silent := logging.From(isolatedLoggerContext(t.Context(), nil))
	require.False(t, silent.Enabled(ctx, slog.LevelInfo), "audit logging is on for a host that configured no logger")

	configured := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, got := logging.From(isolatedLoggerContext(t.Context(), configured))
	require.Same(t, configured, got, "the configured logger is used when the context carries none")

	bound := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, got = logging.From(isolatedLoggerContext(logging.WithLogger(t.Context(), bound), configured))
	require.Same(t, bound, got, "a logger the host bound into the context wins over the configured one")
}

func TestLaunchRecordLifecycle(t *testing.T) {
	t.Parallel()
	state := privateTestDirectory(t)
	record, err := createLaunchRecord(state, "tenant-a", false)
	require.NoError(t, err)
	require.False(t, record.journal, "worker record carries a journal: %+v", record)
	require.Empty(t, record.journalDirectory())
	require.Len(t, record.storeMetadata(), 2)
	for _, directory := range []string{record.checkpointDirectory(), record.approvalDirectory(), record.imagesDirectory()} {
		info, err := os.Stat(directory)
		require.NoError(t, err, "record child %s", directory)
		require.True(t, info.IsDir())
		require.Equal(t, os.FileMode(launchRecordMode), info.Mode().Perm())
	}
	_, err = os.Stat(filepath.Join(record.directory, launchRecordJournal))
	require.ErrorIs(t, err, os.ErrNotExist, "worker record created a journal directory")
	filesystem, err := createLaunchRecord(state, "tenant-a", true)
	require.NoError(t, err)
	require.True(t, filesystem.journal, "filesystem record lacks its journal: %+v", filesystem)
	require.NotEmpty(t, filesystem.journalDirectory())
	require.Len(t, filesystem.storeMetadata(), 3)
	other, err := createLaunchRecord(state, "tenant-b", false)
	require.NoError(t, err)
	require.NotEqual(t, filepath.Dir(other.directory), filepath.Dir(record.directory), "tenants share a record directory")
	records, err := listLaunchRecords(state, "tenant-a")
	require.NoError(t, err, "tenant-a")
	require.Len(t, records, 2)
	journals := 0
	for _, listed := range records {
		if listed.journal {
			journals++
		}
	}
	require.Equal(t, 1, journals, "the listing keeps the journal marker: %v", records)
	listed, err := listLaunchRecords(state, "tenant-c")
	require.NoError(t, err, "unknown tenant")
	require.Empty(t, listed)
	require.NoError(t, record.remove())
	records, err = listLaunchRecords(state, "tenant-a")
	require.NoError(t, err, "removal left the wrong record")
	require.Len(t, records, 1)
	require.True(t, records[0].journal)
	require.NotEqual(t, tenantSlug("tenant-a"), tenantSlug("tenant-b"), "tenant slugs are not distinct or wrongly sized")
	require.Len(t, tenantSlug("x"), tenantSlugLength)
}

func TestLaunchRecordsRequirePrivateState(t *testing.T) {
	t.Parallel()
	shared := t.TempDir()
	require.NoError(t, os.Chmod(shared, 0o755))
	_, err := createLaunchRecord(shared, "tenant", false)
	require.ErrorIs(t, err, ErrInvalidIsolatedConfig, "shared state directory accepted")
	_, err = listLaunchRecords(shared, "tenant")
	require.ErrorIs(t, err, ErrInvalidIsolatedConfig, "shared state directory listed")
	missing := filepath.Join(t.TempDir(), "missing")
	_, err = createLaunchRecord(missing, "tenant", false)
	require.ErrorIs(t, err, ErrInvalidIsolatedConfig, "missing state directory accepted")
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	_, err = createLaunchRecord(file, "tenant", false)
	require.ErrorIs(t, err, ErrInvalidIsolatedConfig, "regular file accepted as state directory")
	require.NoError(t, launchRecordError(nil))
	require.ErrorIs(t, launchRecordError(os.ErrPermission), ErrIsolatedUnavailable, "record failure not mapped to unavailability")
}

func TestLaunchRecordsAreBounded(t *testing.T) {
	t.Parallel()
	state := privateTestDirectory(t)
	tenant := filepath.Join(state, tenantSlug("tenant"))
	require.NoError(t, os.Mkdir(tenant, launchRecordMode))
	for index := range maximumLaunchRecords + 1 {
		require.NoError(t, os.Mkdir(filepath.Join(tenant, "record-"+strconv.Itoa(index)), launchRecordMode))
	}
	require.NoError(t, os.WriteFile(filepath.Join(tenant, "stray"), nil, 0o600))
	_, err := listLaunchRecords(state, "tenant")
	require.ErrorIs(t, err, errLaunchRecords, "record bound not enforced")
	require.NoError(t, os.Remove(filepath.Join(tenant, "record-0")))
	records, err := listLaunchRecords(state, "tenant")
	require.NoError(t, err, "bounded listing")
	require.Len(t, records, maximumLaunchRecords)
	for index := 1; index < len(records); index++ {
		require.Less(t, records[index-1].directory, records[index].directory, "records are not sorted")
	}
}

func privateTestDirectory(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	require.NoError(t, os.Chmod(directory, launchRecordMode))
	return directory
}
