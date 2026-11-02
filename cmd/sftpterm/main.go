// Command sftpterm is a delightful terminal UI for transferring files over SFTP.
package main

import (
	"context"
	"os"

	"charm.land/fang/v2"
	"github.com/spf13/cobra"

	"github.com/jamaalsawyerd/sftpterm/internal/ui"
)

// Populated via -ldflags at release time. When empty, fang falls back to the
// module build info, so `go install` builds still report a useful version.
var (
	version = ""
	commit  = ""
)

func main() {
	root := &cobra.Command{
		Use:   "sftpterm",
		Short: "A delightful terminal UI for transferring files over SFTP",
		Long: "sftpterm is a dual-pane terminal file manager for moving files " +
			"between your machine and any SFTP server.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return ui.Run(cmd.Context())
		},
	}

	if err := fang.Execute(
		context.Background(),
		root,
		fang.WithVersion(version),
		fang.WithCommit(commit),
	); err != nil {
		os.Exit(1)
	}
}
