// Package queue is the single writer: hooks drop event files into
// queue/, and Drain folds them in id order while holding queue/.lock.
package queue

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/calebtonn/coal/internal/capture"
	"github.com/calebtonn/coal/internal/manifest"
)

// Dir returns the queue directory under the data dir.
func Dir(dataDir string) string { return filepath.Join(dataDir, "queue") }

// Enqueue writes e to queue/<id>.json under dataDir with a temp file and
// a rename, so a drain never sees a partial event. It assigns an id and
// timestamp when e has none.
func Enqueue(dataDir string, e *capture.Event) error {
	if e.ID == "" {
		e.ID = manifest.NewID()
	}
	if e.At == "" {
		e.At = manifest.Now()
	}
	if strings.ContainsAny(e.ID, `/\.`) {
		return fmt.Errorf("queue: bad event id %q", e.ID)
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	dir := Dir(dataDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".enqueue-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(b)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmp, filepath.Join(dir, e.ID+".json"))
	}
	if werr != nil {
		os.Remove(tmp)
	}
	return werr
}

// Drain takes queue/.lock and applies every queued event in id order,
// which is time order for UUIDv7. Each event file is deleted after a
// successful apply. An event that fails to parse or apply is moved to
// queue/failed/ and logged, and the drain carries on. Drain repeats
// until the queue is empty, so events that arrive meanwhile are folded
// too. When wait is false and another process holds the lock, Drain
// returns 0, nil at once.
func Drain(dataDir string, apply func(*capture.Event) error, wait bool) (n int, err error) {
	how := syscall.LOCK_EX
	if !wait {
		how |= syscall.LOCK_NB
	}
	n, _, err = drain(dataDir, apply, func(f *os.File) error { return flock(f, how) })
	return n, err
}

// DrainUntil is Drain that waits for the lock only until deadline. busy
// reports that the lock never came free, so nothing was folded.
func DrainUntil(dataDir string, apply func(*capture.Event) error, deadline time.Time) (n int, busy bool, err error) {
	return drain(dataDir, apply, func(f *os.File) error {
		for {
			err := flock(f, syscall.LOCK_EX|syscall.LOCK_NB)
			if !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
				return err
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
}

func drain(dataDir string, apply func(*capture.Event) error, take func(*os.File) error) (n int, busy bool, err error) {
	dir := Dir(dataDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, false, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return 0, false, err
	}
	defer lock.Close()
	if err := take(lock); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return 0, true, nil
		}
		return 0, false, fmt.Errorf("queue lock: %w", err)
	}
	defer flock(lock, syscall.LOCK_UN)

	for {
		names, err := pending(dir)
		if err != nil || len(names) == 0 {
			return n, false, err
		}
		for _, name := range names {
			path := filepath.Join(dir, name)
			if ferr := applyFile(path, apply); ferr != nil {
				logf(dataDir, "queue: %s: %v", name, ferr)
				if merr := moveFailed(dir, name); merr != nil {
					return n, false, fmt.Errorf("queue: cannot set aside %s: %w", name, merr)
				}
				continue
			}
			if err := os.Remove(path); err != nil {
				return n, false, fmt.Errorf("queue: cannot remove folded %s: %w", name, err)
			}
			n++
		}
	}
}

func flock(f *os.File, how int) error {
	for {
		err := syscall.Flock(int(f.Fd()), how)
		if err != syscall.EINTR {
			return err
		}
	}
}

// pending lists queued event file names in lexical order.
func pending(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range ents {
		if name := e.Name(); e.Type().IsRegular() && strings.HasSuffix(name, ".json") && !strings.HasPrefix(name, ".") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func applyFile(path string, apply func(*capture.Event) error) (err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var e capture.Event
	if err := json.Unmarshal(b, &e); err != nil {
		return fmt.Errorf("bad event: %w", err)
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("apply panicked: %v", r)
		}
	}()
	return apply(&e)
}

func moveFailed(dir, name string) error {
	failed := filepath.Join(dir, "failed")
	if err := os.MkdirAll(failed, 0o700); err != nil {
		return err
	}
	return os.Rename(filepath.Join(dir, name), filepath.Join(failed, name))
}

// logf appends one line to logs/coal.log under the data dir. Logging is
// best effort and never fails a drain.
func logf(dataDir, format string, args ...any) {
	logs := filepath.Join(dataDir, "logs")
	if os.MkdirAll(logs, 0o700) != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(logs, "coal.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s "+format+"\n", append([]any{time.Now().UTC().Format(time.RFC3339)}, args...)...)
}
