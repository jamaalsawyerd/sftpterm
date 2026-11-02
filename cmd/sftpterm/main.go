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
	"github.com/jamaalsawyerd/sftpterm/internal/sshx"
	"github.com/jamaalsawyerd/sftpterm/internal/state"
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
	port       int
}

func main() {
	var f flags
	defaultConfig, _ := config.DefaultPath()
	logPath := debugLogPath()

	root := &cobra.Command{
		Use:   "sftpterm [[user@]host[:path] | sftp://[user@]host[:port][/path]]",
		Short: "A delightful terminal UI for transferring files over SFTP",
		Long: "sftpterm is a dual-pane terminal file manager for moving files " +
			"between your machine and any SFTP server.\n\n" +
			"Run it without arguments to pick a host from ~/.ssh/config, or pass " +
			"a destination to connect straight away.",
		Example: "  sftpterm\n  sftpterm nas\n  sftpterm jsd@nas:/srv/media\n  sftpterm -P 2222 pi@raspberrypi.local",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd.Context(), f, logPath, args)
		},
	}
	root.Flags().StringVarP(&f.configPath, "config", "c", defaultConfig, "path to the config file")
	root.Flags().StringVar(&f.theme, "theme", "", "color theme: "+strings.Join(theme.Names(), ", "))
	root.Flags().BoolVar(&f.debug, "debug", false, "write a debug log to "+logPath)
	root.Flags().IntVarP(&f.port, "port", "P", 0, "port to connect to (overrides ssh config)")

	if err := fang.Execute(
		context.Background(),
		root,
		fang.WithVersion(version),
		fang.WithCommit(commit),
	); err != nil {
		os.Exit(1)
	}
}

func run(ctx context.Context, f flags, logPath string, args []string) error {
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
	sshcfg, err := sshx.LoadSSHConfig(sshx.DefaultSSHConfigPath())
	if err != nil {
		warnings = append(warnings, err.Error())
		sshcfg = sshx.EmptySSHConfig()
	}

	store := loadState(&warnings)
	opts := ui.Options{Config: cfg, Warnings: warnings, SSHConfig: sshcfg, ConfigPath: f.configPath, State: store}
	if len(args) == 1 {
		dest, err := sshx.ParseDestination(args[0])
		if err != nil {
			return err
		}
		if f.port != 0 {
			if f.port < 1 || f.port > 65535 {
				return fmt.Errorf("invalid port %d", f.port)
			}
			dest.Port = f.port
		}
		opts.Destination = &dest
	} else if f.port != 0 {
		return fmt.Errorf("a destination is required when using --port")
	}
	log.Debug("starting", "config", f.configPath, "theme", cfg.Theme, "warnings", warnings, "destination", args)

	return ui.Run(ctx, opts)
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

// loadState reads bookmarks next to the config file. Problems become
// warnings: losing bookmarks shouldn't stop the app.
func loadState(warnings *[]string) *state.Store {
	path, err := state.DefaultPath()
	if err != nil {
		*warnings = append(*warnings, err.Error())
		return nil
	}
	store, err := state.Load(path)
	if err != nil {
		*warnings = append(*warnings, err.Error())
	}
	return store
}
