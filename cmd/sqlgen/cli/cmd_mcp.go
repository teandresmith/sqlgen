package cli

import (
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/teandresmith/sqlgen/cmd/sqlgen/mcp"
)

// mcpServeFlags holds the `sqlgen mcp serve` flag values (MCP.md §3.1).
type mcpServeFlags struct {
	manifest string
	stdio    bool
	httpAddr string
	watch    bool
	noWatch  bool
	logPath  string
	logLevel string
}

// newMCPCmd builds the `sqlgen mcp` parent command and its `serve` subcommand.
// The server is a read-only consumer of the generated manifest, exposing it to
// AI agents over MCP (PRD §31 / MCP.md).
func newMCPCmd(_ *cliFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve the generated manifest to AI agents over MCP",
	}
	cmd.AddCommand(newMCPServeCmd())
	return cmd
}

func newMCPServeCmd() *cobra.Command {
	f := &mcpServeFlags{}
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the manifest over MCP (stdio by default, optional loopback HTTP)",
		Long: "Run the Model Context Protocol server for a generated manifest.\n\n" +
			"stdio is the default transport (agent clients spawn the server per session).\n" +
			"--http <port> additionally serves Streamable HTTP bound to 127.0.0.1 only;\n" +
			"any non-loopback bind address is refused.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMCPServe(cmd, f)
		},
	}
	cmd.Flags().StringVar(&f.manifest, "manifest", "", "path to manifest_gen.json (default: walk up from CWD)")
	cmd.Flags().BoolVar(&f.stdio, "stdio", true, "serve over stdio (JSON-RPC 2.0 on stdin/stdout)")
	cmd.Flags().StringVar(&f.httpAddr, "http", "", "also serve Streamable HTTP on 127.0.0.1:<port> (loopback only)")
	cmd.Flags().BoolVar(&f.watch, "watch", true, "reload the manifest on change")
	cmd.Flags().BoolVar(&f.noWatch, "no-watch", false, "disable manifest watch mode (daemon/production use)")
	cmd.Flags().StringVar(&f.logPath, "log", "", "log to file (default: stderr — stdout is reserved for JSON-RPC)")
	cmd.Flags().StringVar(&f.logLevel, "log-level", "info", "log level: debug|info|warn|error")
	cmd.MarkFlagsMutuallyExclusive("watch", "no-watch")
	return cmd
}

func runMCPServe(cmd *cobra.Command, f *mcpServeFlags) error {
	// Resolve the log destination: stderr by default, --log redirects to a
	// file. stdout is never a target — it carries JSON-RPC framing.
	logW := io.Writer(os.Stderr)
	var logFile *os.File
	if f.logPath != "" {
		file, err := os.OpenFile(f.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return &exitError{code: ExitConfig, err: fmt.Errorf("open --log %s: %w", f.logPath, err)}
		}
		logW, logFile = file, file
	}

	logger, err := mcp.NewLogger(logW, f.logLevel)
	if err != nil {
		if logFile != nil {
			_ = logFile.Close()
		}
		return &exitError{code: ExitConfig, err: err}
	}

	// Refuse a non-loopback --http bind up front (flag-parse time).
	var httpAddr string
	if f.httpAddr != "" {
		addr, err := mcp.ResolveHTTPAddr(f.httpAddr)
		if err != nil {
			if logFile != nil {
				_ = logFile.Close()
			}
			return &exitError{code: ExitConfig, err: err}
		}
		httpAddr = addr
	}

	// SIGINT cancels the context for a graceful shutdown.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	runErr := mcp.Run(ctx, os.Stdin, os.Stdout, mcp.Options{
		Version:      Version,
		ManifestPath: f.manifest,
		Stdio:        f.stdio,
		HTTPAddr:     httpAddr,
		Watch:        f.watch && !f.noWatch,
		Logger:       logger,
	})

	// Flush logs before exiting (close the --log file if we opened one).
	if logFile != nil {
		_ = logFile.Close()
	}
	if runErr != nil {
		return &exitError{code: ExitGeneration, err: runErr}
	}
	return nil
}
