package summarize

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/calebtonn/coal/internal/capture"
)

// Config is config.summarizer: an opt-in headless model call over the
// transcript (ADR-016). It is a call the user configured; nothing else
// in COAL calls a model.
type Config struct {
	Command            []string `json:"command"`
	TimeoutSeconds     int      `json:"timeoutSeconds"`
	MaxTranscriptBytes int64    `json:"maxTranscriptBytes"`

	// WorkRoot holds the empty working directory made for each run.
	// RunJobs sets it to the data dir's tmp/; empty means os.TempDir.
	WorkRoot string `json:"-"`
}

// EnvSummarizer is set to 1 in the summarizer's environment. A summarizer
// such as `claude -p` is itself a Claude Code session with the user's
// hooks, so coal hook exits at once when it sees this, and the run never
// queues another summarize job (Fable review M11, R1).
const EnvSummarizer = "COAL_SUMMARIZER"

// Defaults for unset Config fields.
const (
	DefaultTimeoutSeconds     = 120
	DefaultMaxTranscriptBytes = 400_000
	maxOutput                 = 64 << 10
)

// Prompt goes on stdin ahead of the transcript.
const Prompt = `You are writing a short entry in the user's record of work. Below is the end of a Claude Code session transcript, one JSON object per line. Read it and reply with exactly two lines and nothing else:
Summary: <one or two plain sentences on what was done in this session>
Next step: <the next concrete action, or "none">

Transcript:
`

// LoadConfig reads config.summarizer from home/config.json. It returns
// nil when the file or the key is missing, null or false.
func LoadConfig(home string) (*Config, error) {
	b, err := os.ReadFile(filepath.Join(home, "config.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var raw struct {
		Summarizer json.RawMessage `json:"summarizer"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("config.json: %w", err)
	}
	s := bytes.TrimSpace(raw.Summarizer)
	if len(s) == 0 || string(s) == "null" || string(s) == "false" {
		return nil, nil
	}
	var c Config
	if err := json.Unmarshal(s, &c); err != nil {
		return nil, fmt.Errorf("config.json summarizer: %w", err)
	}
	if len(c.Command) == 0 || c.Command[0] == "" {
		return nil, errors.New("config.json summarizer: command must name a program")
	}
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = DefaultTimeoutSeconds
	}
	if c.MaxTranscriptBytes <= 0 {
		c.MaxTranscriptBytes = DefaultMaxTranscriptBytes
	}
	return &c, nil
}

// ErrNoResult means the summarizer ran but gave nothing usable. Errors
// from Run never carry transcript text, so they are safe to log.
var ErrNoResult = errors.New("summarizer output had no Summary line")

// Run starts cfg.Command with no shell, writes Prompt and the tail of the
// transcript to its stdin, and parses "Summary:" and "Next step:" from
// its stdout. The transcript never goes on the command line. A non-zero
// exit, a timeout or output without a summary is an error.
func Run(ctx context.Context, cfg *Config, transcriptPath string) (summary, nextStep string, err error) {
	body, err := tail(transcriptPath, cfg.MaxTranscriptBytes)
	if err != nil {
		return "", "", fmt.Errorf("reading transcript: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	// Run in a fresh empty directory, so no project marker or context
	// matches the summarizer's own session.
	if cfg.WorkRoot != "" {
		if err := os.MkdirAll(cfg.WorkRoot, 0o700); err != nil {
			return "", "", err
		}
	}
	work, err := os.MkdirTemp(cfg.WorkRoot, "summarizer-*")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(work)
	cmd := exec.CommandContext(ctx, cfg.Command[0], cfg.Command[1:]...)
	cmd.Dir = work
	cmd.Env = append(os.Environ(), EnvSummarizer+"=1")
	cmd.Stdin = io.MultiReader(strings.NewReader(Prompt), bytes.NewReader(body))
	var out limited
	cmd.Stdout, cmd.Stderr = &out, io.Discard
	// Kill the whole process group, so a child that holds stdout open
	// cannot outlive the timeout.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", "", fmt.Errorf("summarizer timed out after %ds", cfg.TimeoutSeconds)
	}
	if err != nil {
		return "", "", fmt.Errorf("summarizer %s: %w", filepath.Base(cfg.Command[0]), err)
	}
	summary, nextStep = parseOutput(out.Bytes())
	if summary == "" {
		return "", "", ErrNoResult
	}
	return summary, nextStep, nil
}

// parseOutput takes the first "Summary:" and "Next step:" lines, case
// insensitive, ignoring markdown bold and bullets. Everything else is
// ignored. "none" as a next step means no next step.
func parseOutput(b []byte) (summary, nextStep string) {
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		l := strings.TrimLeft(strings.TrimSpace(sc.Text()), "-*> ")
		l = strings.Replace(l, ":**", ":", 1)
		key, val, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		val = Cut(strings.Trim(val, " *"), capture.MaxStateSummary)
		switch k := strings.ToLower(strings.TrimSpace(key)); {
		case k == "summary" && summary == "":
			summary = val
		case (k == "next step" || k == "next") && nextStep == "":
			if !strings.EqualFold(strings.Trim(val, `".`), "none") {
				nextStep = val
			}
		}
	}
	return summary, nextStep
}

// tail returns at most max bytes from the end of path, starting after
// the first partial line when it had to cut.
func tail(path string, max int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("transcript is not a regular file")
	}
	off := int64(0)
	if max > 0 && fi.Size() > max {
		off = fi.Size() - max
	}
	b := make([]byte, fi.Size()-off)
	n, err := f.ReadAt(b, off)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	b = b[:n]
	if off > 0 {
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		} else {
			b = nil
		}
	}
	return b, nil
}

// limited keeps the first maxOutput bytes and drops the rest.
type limited struct{ bytes.Buffer }

func (l *limited) Write(p []byte) (int, error) {
	if room := maxOutput - l.Len(); room > 0 {
		l.Buffer.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}
