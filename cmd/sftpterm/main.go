// Command sftpterm is a delightful terminal UI for transferring files over SFTP.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"charm.land/fang/v2"
	"charm.land/log/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/spf13/cobra"

	"github.com/jamaalsawyerd/sftpterm/internal/config"
	"github.com/jamaalsawyerd/sftpterm/internal/ui"
	"github.com/jamaalsawyerd/sftpterm/internal/ui/theme"
)

// Populated via -ldflags at release time. When empty, fang falls back to the
// module build info, so `go install` builds still report a useful version.
var (
	version = ""
	commit  = ""
)

type flags struct {
	configPath string
	theme      string
	debug      bool
}

func main() {
	var f flags
	defaultConfig, _ := config.DefaultPath()
	logPath := debugLogPath()

	root := &cobra.Command{
		Use:   "sftpterm",
		Short: "A delightful terminal UI for transferring files over SFTP",
		Long: "sftpterm is a dual-pane terminal file manager for moving files " +
			"between your machine and any SFTP server.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd.Context(), f, logPath)
		},
	}
	root.Flags().StringVarP(&f.configPath, "config", "c", defaultConfig, "path to the config file")
	root.Flags().StringVar(&f.theme, "theme", "", "color theme: "+strings.Join(theme.Names(), ", "))
	root.Flags().BoolVar(&f.debug, "debug", false, "write a debug log to "+logPath)

	if err := fang.Execute(
		context.Background(),
		root,
		fang.WithVersion(version),
		fang.WithCommit(commit),
	); err != nil {
		os.Exit(1)
	}
}

func run(ctx context.Context, f flags, logPath string) error {
	closeLog, err := setupLogging(f.debug, logPath)
	if err != nil {
		return err
	}
	defer closeLog()

	if f.configPath == "" {
		return fmt.Errorf("no config path: set --config")
	}
	cfg, warnings, err := config.Load(f.configPath)
	if err != nil {
		return err
	}
	if f.theme != "" {
		cfg.Theme = f.theme
	}
	log.Debug("starting", "config", f.configPath, "theme", cfg.Theme, "warnings", warnings)

	return ui.Run(ctx, ui.Options{Config: cfg, Warnings: warnings})
}

// setupLogging routes the default logger to a file when debugging. Otherwise
// logs are discarded: anything written to stderr would corrupt the TUI.
func setupLogging(debug bool, path string) (func(), error) {
	if !debug {
		log.SetOutput(io.Discard)
		return func() {}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("creating log directory: %w", err)
	}
	fh, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening debug log: %w", err)
	}
	log.SetOutput(fh)
	log.SetColorProfile(colorprofile.NoTTY) // plain text in the file
	log.SetLevel(log.DebugLevel)
	log.SetReportTimestamp(true)
	return func() { _ = fh.Close() }, nil
}

func debugLogPath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "sftpterm", "debug.log")
}
