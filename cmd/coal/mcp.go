package main

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/calebtonn/coal/internal/mcpserver"
)

func init() { register(mcpCmd) }

func mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP server on stdio for AI tools",
		Long:  "Run the MCP server on stdin and stdout. Only protocol goes to stdout; logs go to logs/mcp.log in the data directory.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, _, err := openApp("")
			if err != nil {
				return err
			}
			defer a.Close()
			srv, closeLog, err := mcpserver.New(a)
			if err != nil {
				return err
			}
			defer closeLog()
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			err = srv.Run(ctx, &mcp.StdioTransport{})
			if ctx.Err() != nil {
				return nil
			}
			return err
		},
	}
}
