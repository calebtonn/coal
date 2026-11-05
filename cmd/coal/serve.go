package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/calebtonn/coal/internal/capture"
	"github.com/calebtonn/coal/internal/daemon"
	"github.com/calebtonn/coal/internal/fold"
	"github.com/calebtonn/coal/internal/queue"
	"github.com/calebtonn/coal/web"
)

func init() { register(serveCmd) }

func serveCmd() *cobra.Command {
	var port int
	var host string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the local daemon and web UI on 127.0.0.1",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Flags().Changed("host") {
				return errors.New("coal serve binds 127.0.0.1 only; --host is not supported")
			}
			if port < 1 || port > 65535 {
				return fmt.Errorf("--port must be between 1 and 65535, got %d", port)
			}
			a, _, err := openApp("")
			if err != nil {
				return err
			}
			defer a.Close()
			dist, err := fs.Sub(web.Dist, "dist")
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return daemon.Serve(ctx, a, fmt.Sprintf("127.0.0.1:%d", port), daemon.Options{
				UI: dist,
				Tick: func() {
					if _, err := queue.Drain(a.Store.Dir(), func(e *capture.Event) error { return fold.Apply(a, e) }, false); err != nil {
						a.Store.Log("daemon: queue drain: %v", err)
					}
				},
				Ready: func(addr net.Addr) {
					fmt.Fprintf(cmd.OutOrStdout(), "COAL is running at http://%s\n", addr)
				},
			})
		},
	}
	cmd.Flags().IntVar(&port, "port", 4242, "port on 127.0.0.1")
	cmd.Flags().StringVar(&host, "host", "", "not supported; the daemon binds 127.0.0.1 only")
	cmd.Flags().MarkHidden("host")
	return cmd
}
