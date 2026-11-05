package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/calebtonn/coal/internal/paths"
)

// adapterHooks lists the Claude Code hooks COAL installs.
var adapterHooks = []struct {
	event, arg, matcher string
	timeout             int
}{
	{"SessionStart", "session-start", "", 0},
	{"UserPromptSubmit", "user-prompt-submit", "", 0},
	{"PostToolUse", "post-tool-use", "Write|Edit|MultiEdit|NotebookEdit", 0},
	{"Stop", "stop", "", 0},
	{"SessionEnd", "session-end", "", 5},
}

func init() { register(adapterCmd) }

func adapterCmd() *cobra.Command {
	ad := &cobra.Command{Use: "adapter", Short: "Install or remove tool adapters"}
	for _, install := range []bool{true, false} {
		var settings string
		use, short := "uninstall claude-code --settings <file>", "Remove the COAL hooks from a Claude Code settings file"
		if install {
			use, short = "install claude-code --settings <file>", "Add the COAL hooks to a Claude Code settings file"
		}
		c := &cobra.Command{
			Use:   use,
			Short: short,
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				if args[0] != "claude-code" {
					return fmt.Errorf("unknown adapter %q; the POC has claude-code only", args[0])
				}
				if settings == "" {
					return errors.New("--settings is required in the POC, so COAL never edits ~/.claude/ on its own")
				}
				bin, err := os.Executable()
				if err != nil {
					return err
				}
				if r, err := filepath.EvalSymlinks(bin); err == nil {
					bin = r
				}
				home := ""
				if homeFlag != "" || os.Getenv(paths.EnvHome) != "" {
					if home, err = dataHome(); err != nil {
						return err
					}
				}
				if err := editSettings(settings, bin, home, install); err != nil {
					return err
				}
				verb := "removed COAL hooks from"
				if install {
					verb = "installed COAL hooks in"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", verb, settings)
				return nil
			},
		}
		c.Flags().StringVar(&settings, "settings", "", "Claude Code settings file to edit (required)")
		ad.AddCommand(c)
	}
	return ad
}

// shellQuote quotes s for bash when it holds anything but safe characters.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._-+=:,@") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// hookCommand is the shell command one hook runs.
func hookCommand(bin, home, arg string) string {
	cmd := shellQuote(bin) + " hook " + arg
	if home != "" {
		cmd = paths.EnvHome + "=" + shellQuote(home) + " " + cmd
	}
	return cmd
}

// isOurs reports whether a hook handler runs this coal binary's hook.
func isOurs(h any, bin string) bool {
	m, ok := h.(map[string]any)
	if !ok {
		return false
	}
	c, _ := m["command"].(string)
	return strings.Contains(c, " hook ") && (strings.Contains(c, bin) || strings.Contains(c, shellQuote(bin)))
}

// editSettings removes this binary's hooks from the settings file and,
// when install is set, adds them back, so install is idempotent. Other
// settings and hooks are kept. The previous content goes to <file>.bak
// and the new file is written atomically.
func editSettings(path, bin, home string, install bool) error {
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	settings := map[string]any{}
	if len(bytes.TrimSpace(old)) > 0 {
		if err := json.Unmarshal(old, &settings); err != nil {
			return fmt.Errorf("%s is not valid JSON: %w", path, err)
		}
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		if _, exists := settings["hooks"]; exists {
			return fmt.Errorf("%s: \"hooks\" is not an object", path)
		}
		hooks = map[string]any{}
	}
	for event, v := range hooks {
		groups, ok := v.([]any)
		if !ok {
			continue
		}
		var keep []any
		for _, g := range groups {
			gm, ok := g.(map[string]any)
			hs, _ := gm["hooks"].([]any)
			if !ok || hs == nil {
				keep = append(keep, g)
				continue
			}
			var rest []any
			for _, h := range hs {
				if !isOurs(h, bin) {
					rest = append(rest, h)
				}
			}
			if len(rest) > 0 {
				gm["hooks"] = rest
				keep = append(keep, gm)
			}
		}
		if len(keep) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = keep
		}
	}
	if install {
		for _, h := range adapterHooks {
			handler := map[string]any{"type": "command", "command": hookCommand(bin, home, h.arg)}
			if h.timeout > 0 {
				handler["timeout"] = h.timeout
			}
			group := map[string]any{"hooks": []any{handler}}
			if h.matcher != "" {
				group["matcher"] = h.matcher
			}
			groups, _ := hooks[h.event].([]any)
			hooks[h.event] = append(groups, group)
		}
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooks
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if bytes.Equal(data, old) {
		return nil
	}
	if old != nil {
		if err := writeFileAtomic(path+".bak", old); err != nil {
			return err
		}
	}
	return writeFileAtomic(path, data)
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := fs.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Chmod(mode)
	}
	if err = errors.Join(err, f.Close()); err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}
