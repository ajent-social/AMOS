package backup

import (
	"context"
	"errors"
	"io"
	"math"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

func scheduleOwned(info os.FileInfo, dir bool) bool {
	if info == nil || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return false
	}
	if dir && !info.IsDir() || !dir && !info.Mode().IsRegular() {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uint32(os.Geteuid())
}

// stagingCapacity never traverses unknown directories or deletes retained work.
// It counts only fixed known direct artifact files and verifies prior attempt IDs
// against this stream's actual ledger before treating the staging data as owned.
func (s *Scheduler) stagingCapacity(ctx context.Context) (bool, error) {
	if ctx == nil || ctx.Err() != nil {
		return false, ErrScheduleUnavailable
	}
	root, e := os.Lstat(s.cfg.Create.StagingDir)
	if e != nil || !scheduleOwned(root, true) {
		return false, ErrScheduleUnavailable
	}
	f, e := os.Open(s.cfg.Create.StagingDir)
	if e != nil {
		return false, ErrScheduleUnavailable
	}
	defer func() { _ = f.Close() }()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(root, opened) {
		return false, ErrScheduleUnavailable
	}
	entries, e := f.ReadDir(int(s.cfg.MaxStagingArtifacts) + 1)
	if e != nil && !errors.Is(e, io.EOF) {
		return false, ErrScheduleUnavailable
	}
	if len(entries) >= int(s.cfg.MaxStagingArtifacts) {
		return false, nil
	}
	var total int64
	var ids []string
	for _, entry := range entries {
		if ctx.Err() != nil {
			return false, ErrScheduleUnavailable
		}
		name := strings.TrimPrefix(entry.Name(), "backup-")
		id, e := uuid.Parse(name)
		if e != nil || !strings.HasPrefix(entry.Name(), "backup-") || id.String() != name || !scheduleUUID(id) {
			return false, ErrScheduleUnavailable
		}
		info, e := entry.Info()
		if e != nil || !scheduleOwned(info, true) {
			return false, ErrScheduleUnavailable
		}
		fd, e := unix.Openat(int(f.Fd()), entry.Name(), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
		if e != nil {
			return false, ErrScheduleUnavailable
		}
		dir := os.NewFile(uintptr(fd), entry.Name())
		files, e := dir.ReadDir(7)
		if e != nil && !errors.Is(e, io.EOF) || len(files) > 6 {
			_ = dir.Close()
			return false, ErrScheduleUnavailable
		}
		for _, file := range files {
			limit := int64(65536)
			switch file.Name() {
			case "database.dump", "database.dump.partial":
				limit = s.cfg.Create.MaxBytes
			case "manifest.json", "manifest.json.partial":
				limit = 1 << 20
			case ".pg_service.conf", ".pgpass":
			default:
				_ = dir.Close()
				return false, ErrScheduleUnavailable
			}
			var child unix.Stat_t
			e := unix.Fstatat(int(dir.Fd()), file.Name(), &child, unix.AT_SYMLINK_NOFOLLOW)
			if e != nil || child.Mode&unix.S_IFMT != unix.S_IFREG || child.Mode&0077 != 0 || child.Uid != uint32(os.Geteuid()) || child.Size < 0 || child.Size > limit || total > math.MaxInt64-child.Size {
				_ = dir.Close()
				return false, ErrScheduleUnavailable
			}
			total += child.Size
		}
		if dir.Close() != nil {
			return false, ErrScheduleUnavailable
		}
		ids = append(ids, name)
	}
	after, e := os.Lstat(s.cfg.Create.StagingDir)
	if e != nil || !os.SameFile(root, after) {
		return false, ErrScheduleUnavailable
	}
	reserve := s.cfg.Create.MaxBytes + (2 << 20)
	if total > s.cfg.MaxStagingBytes-reserve {
		return false, nil
	}
	var stat unix.Statfs_t
	if unix.Fstatfs(int(f.Fd()), &stat) != nil {
		return false, ErrScheduleUnavailable
	}
	if runtime.GOOS == "linux" {
		switch uint64(stat.Type) {
		case 0x6969, 0x65735546, 0xff534d42, 0x517b, 0x01021997, 0x5346414f:
			return false, ErrScheduleUnavailable
		}
	}
	if stat.Bsize <= 0 || uint64(stat.Bavail) > math.MaxUint64/uint64(stat.Bsize) {
		return false, ErrScheduleUnavailable
	}
	if uint64(stat.Bavail)*uint64(stat.Bsize) < uint64(reserve) {
		return false, nil
	}
	if len(ids) > 0 {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		tx, c, stop, e := beginScheduleTx(ctx, s.db, s.cfg, false)
		if e != nil {
			return false, e
		}
		defer stop()
		defer func() { _ = tx.Rollback() }()
		var count int
		if tx.QueryRowContext(c, `SELECT count(*) FROM public.amos_backup_attempts WHERE schedule_id=$1 AND attempt_id=ANY($2::uuid[])`, s.cfg.ScheduleID, "{"+strings.Join(ids, ",")+"}").Scan(&count) != nil || count != len(ids) {
			return false, ErrScheduleUnavailable
		}
		if tx.Commit() != nil {
			return false, ErrScheduleUnavailable
		}
	}
	return true, nil
}
