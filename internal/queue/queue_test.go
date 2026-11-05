package queue

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/calebtonn/coal/internal/capture"
	"github.com/calebtonn/coal/internal/manifest"
)

func ev(kind string) *capture.Event {
	return &capture.Event{ID: manifest.NewID(), Tool: "claude-code", SessionRef: "claude-code:session:s", Kind: kind}
}

func TestDrainOrder(t *testing.T) {
	dir := t.TempDir()
	var want []string
	for range 20 {
		e := ev(capture.KindPrompt)
		want = append(want, e.ID)
		if err := Enqueue(dir, e); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	n, err := Drain(dir, func(e *capture.Event) error { got = append(got, e.ID); return nil }, true)
	if err != nil || n != 20 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("order:\n got %v\nwant %v", got, want)
	}
	left, _ := pending(Dir(dir))
	if len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
	if n, _ := Drain(dir, nil, true); n != 0 {
		t.Errorf("second drain applied %d", n)
	}
}

func TestEnqueueDuringDrain(t *testing.T) {
	dir := t.TempDir()
	if err := Enqueue(dir, ev(capture.KindSessionStart)); err != nil {
		t.Fatal(err)
	}
	var seen []string
	n, err := Drain(dir, func(e *capture.Event) error {
		seen = append(seen, e.Kind)
		if e.Kind == capture.KindSessionStart {
			var wg sync.WaitGroup
			wg.Add(1)
			go func() { defer wg.Done(); _ = Enqueue(dir, ev(capture.KindFileWritten)) }()
			wg.Wait()
		}
		return nil
	}, true)
	if err != nil || n != 2 || len(seen) != 2 || seen[1] != capture.KindFileWritten {
		t.Fatalf("n=%d err=%v seen=%v", n, err, seen)
	}
}

func TestDrainNonBlocking(t *testing.T) {
	dir := t.TempDir()
	if err := Enqueue(dir, ev(capture.KindTurnEnd)); err != nil {
		t.Fatal(err)
	}
	// A separate open file description holds the lock, as another
	// process would.
	f, err := os.OpenFile(filepath.Join(Dir(dir), ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	called := false
	n, err := Drain(dir, func(*capture.Event) error { called = true; return nil }, false)
	if n != 0 || err != nil || called {
		t.Fatalf("busy lock: n=%d err=%v called=%v", n, err, called)
	}
	start := time.Now()
	if n, busy, err := DrainUntil(dir, nil, start.Add(50*time.Millisecond)); n != 0 || !busy || err != nil || time.Since(start) < 50*time.Millisecond {
		t.Fatalf("DrainUntil busy: n=%d busy=%v err=%v after %v", n, busy, err, time.Since(start))
	}
	done := make(chan int)
	go func() { n, _ := Drain(dir, func(*capture.Event) error { return nil }, true); done <- n }()
	select {
	case <-done:
		t.Fatal("waiting drain did not wait for the lock")
	default:
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	if n := <-done; n != 1 {
		t.Errorf("waiting drain applied %d", n)
	}
}

func TestDrainFailed(t *testing.T) {
	dir := t.TempDir()
	bad, good := ev(capture.KindFileWritten), ev(capture.KindTurnEnd)
	_ = Enqueue(dir, bad)
	_ = Enqueue(dir, good)
	_ = os.WriteFile(filepath.Join(Dir(dir), manifest.NewID()+".json"), []byte("{not json"), 0o600)
	var applied []string
	n, err := Drain(dir, func(e *capture.Event) error {
		if e.ID == bad.ID {
			return errors.New("store said no")
		}
		if e.ID == good.ID {
			applied = append(applied, e.ID)
		}
		return nil
	}, true)
	if err != nil || n != 1 || len(applied) != 1 {
		t.Fatalf("n=%d err=%v applied=%v", n, err, applied)
	}
	failed, _ := os.ReadDir(filepath.Join(Dir(dir), "failed"))
	if len(failed) != 2 {
		t.Errorf("failed/ holds %d files", len(failed))
	}
	if _, err := os.Stat(filepath.Join(Dir(dir), "failed", bad.ID+".json")); err != nil {
		t.Error(err)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "logs", "coal.log"))
	if !strings.Contains(string(log), "store said no") || !strings.Contains(string(log), "bad event") {
		t.Errorf("log: %s", log)
	}
}

func TestEnqueueAssignsID(t *testing.T) {
	dir := t.TempDir()
	e := &capture.Event{Kind: capture.KindPrompt}
	if err := Enqueue(dir, e); err != nil || e.ID == "" || e.At == "" {
		t.Fatalf("%+v %v", e, err)
	}
	if err := Enqueue(dir, &capture.Event{ID: "../x"}); err == nil {
		t.Error("path id accepted")
	}
}

func TestDrainUntilFree(t *testing.T) {
	dir := t.TempDir()
	_ = Enqueue(dir, ev(capture.KindPrompt))
	n, busy, err := DrainUntil(dir, func(*capture.Event) error { return nil }, time.Now().Add(time.Second))
	if n != 1 || busy || err != nil {
		t.Fatalf("n=%d busy=%v err=%v", n, busy, err)
	}
}

func TestDrainUnavailable(t *testing.T) {
	dir := t.TempDir()
	for range 3 {
		_ = Enqueue(dir, ev(capture.KindPrompt))
	}
	calls := 0
	n, err := Drain(dir, func(*capture.Event) error {
		calls++
		return fmt.Errorf("open store: %w", ErrUnavailable)
	}, true)
	if n != 0 || !errors.Is(err, ErrUnavailable) || calls != 1 {
		t.Fatalf("n=%d calls=%d err=%v", n, calls, err)
	}
	if left, _ := pending(Dir(dir)); len(left) != 3 {
		t.Errorf("queued: %d", len(left))
	}
	if _, err := os.Stat(filepath.Join(Dir(dir), "failed")); err == nil {
		t.Error("failed/ was used")
	}
}

func TestRetryFailed(t *testing.T) {
	dir := t.TempDir()
	if n, err := RetryFailed(dir); n != 0 || err != nil {
		t.Fatalf("empty: %d %v", n, err)
	}
	e := ev(capture.KindPrompt)
	_ = Enqueue(dir, e)
	Drain(dir, func(*capture.Event) error { return errors.New("bad") }, true)
	if n, err := RetryFailed(dir); n != 1 || err != nil {
		t.Fatalf("retry: %d %v", n, err)
	}
	var got []string
	Drain(dir, func(e *capture.Event) error { got = append(got, e.ID); return nil }, true)
	if len(got) != 1 || got[0] != e.ID {
		t.Errorf("retried: %v", got)
	}
}
