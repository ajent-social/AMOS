package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// This graph qualifies the local SDK composition and actual PostgreSQL/pg_dump
// path. Its private TLS services do not qualify a live cloud identity or bucket.
func TestRuntimeStoreActualSchedulePublication(t *testing.T) {
	for _, mode := range []string{"complete", "partial", "wrong-checksum"} {
		t.Run(mode, func(t *testing.T) {
			initial, reader, _, profile := scheduleFixture(t)
			cfg := initial.cfg
			cfg.AWS = runtimeTestConfig()
			cfg.AWS.CredentialKind = "imds_v2"
			if os.Chmod(cfg.Create.StagingDir, 0700) != nil {
				t.Fatal("fixture staging protection failed")
			}
			cfg.AWS.Prefix = "installations/" + cfg.InstallationID.String() + "/development"
			wireMode := mode
			if mode == "complete" {
				wireMode = ""
			}
			fixture, source, identity, objects := runtimeFixtureClients(t, "imds_v2", wireMode)
			store, err := newRuntimeStoreWithClients(context.Background(), cfg.AWS, cfg.Create.MaxBytes, cfg.RunTimeout+15*time.Second, source, identity, objects)
			if err != nil {
				t.Fatal("runtime identity fixture preflight failed")
			}
			scheduler, err := NewScheduler(initial.db, cfg, store, profile.DumpURL)
			if err != nil {
				t.Fatal("runtime scheduler composition failed")
			}
			reader, err = NewScheduleReader(reader.db, cfg)
			if err != nil {
				t.Fatal("runtime reader composition failed")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 115*time.Second)
			defer cancel()
			result, runErr := scheduler.RunDue(ctx)
			snapshot, err := reader.Snapshot(ctx)
			if err != nil {
				t.Fatal("runtime publication readback unavailable")
			}
			if mode == "complete" {
				if runErr != nil || result != ScheduleCompleted || !snapshot.PublicationKnown || snapshot.State != "completed" || snapshot.LastSnapshotAt.IsZero() || snapshot.LastPublishedAt.Before(snapshot.LastSnapshotAt) {
					t.Fatal("actual dump and two acknowledged PUTs did not establish fenced freshness")
				}
				fixture.mu.Lock()
				var published StoredManifest
				if len(fixture.payloads) != 2 || !bytes.HasPrefix(fixture.payloads[0], []byte("PGDMP")) || json.Unmarshal(fixture.payloads[1], &published) != nil {
					fixture.mu.Unlock()
					t.Fatal("wire did not contain actual PostgreSQL archive and final typed manifest")
				}
				sum := sha256.Sum256(fixture.payloads[0])
				if published.Backup.ArchiveSHA256 != hex.EncodeToString(sum[:]) || !published.Backup.SnapshotTransactionStartedAt.Equal(snapshot.LastSnapshotAt) || published.ArchiveVersionID != "synthetic-version" || published.Backup.ServerVersion == "" {
					fixture.mu.Unlock()
					t.Fatal("wire archive/manifest acknowledgements did not match committed snapshot")
				}
				fixture.mode = "partial"
				fixture.mu.Unlock()
				// Spend a genuinely distinct UTC slot. No clock, slot or attempt is
				// rewritten, and the next partial publication must preserve success.
				next := snapshot.ObservedAt.Truncate(time.Minute).Add(time.Minute + 250*time.Millisecond)
				timer := time.NewTimer(time.Until(next))
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-ctx.Done():
					t.Fatal("next actual scheduling slot deadline exceeded")
				}
				result, err := scheduler.RunDue(ctx)
				if err == nil || result != ScheduleUnknown {
					t.Fatalf("partial second publication result=%s unavailable=%t", result, err != nil)
				}
				failed, err := reader.Snapshot(ctx)
				if err != nil || !failed.PublicationKnown || failed.State != "unknown" || failed.RetainedAttempts != 2 || !failed.LastSnapshotAt.Equal(snapshot.LastSnapshotAt) || !failed.LastPublishedAt.Equal(snapshot.LastPublishedAt) {
					t.Fatal("uncertain distinct-slot publication overwrote the last successful snapshot")
				}
			} else if runErr == nil || snapshot.PublicationKnown || !snapshot.LastSnapshotAt.IsZero() {
				t.Fatal("partial or corrupt publication established freshness")
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			want := 2
			if mode == "complete" {
				want = 4
			}
			if mode == "wrong-checksum" {
				want = 1
			}
			if fixture.bad || fixture.metadata != 1 || fixture.identity != 1 || len(fixture.objects) != want || !strings.HasSuffix(fixture.objects[0], "database.dump") || want > 1 && !strings.HasSuffix(fixture.objects[1], "manifest.json") || want == 4 && (fixture.objects[0] == fixture.objects[2] || !strings.HasSuffix(fixture.objects[2], "database.dump") || !strings.HasSuffix(fixture.objects[3], "manifest.json")) {
				t.Fatal("actual schedule violated snapshot credentials or manifest-last PUT-only wire")
			}
		})
	}
}
