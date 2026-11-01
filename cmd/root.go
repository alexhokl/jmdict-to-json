// Package cmd contains the command-line interface of jmdict-to-json.
package cmd

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/alexhokl/jmdict-to-json/internal/logging"
	"github.com/alexhokl/jmdict-to-json/internal/storage"
)

// app holds what the commands share.
type app struct {
	logFormat  string
	logger     *slog.Logger
	store      *storage.Router
	httpClient *http.Client
}

// Execute runs the command line and exits with status 1 on error.
func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	if err != nil {
		os.Exit(1)
	}
}

// run executes the command line with the given arguments and streams,
// logging any error before returning it.
func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	a := &app{httpClient: http.DefaultClient}
	root := newRootCmd(a)
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.ExecuteContext(ctx)
	if a.store != nil {
		if closeErr := a.store.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		logger := a.logger
		if logger == nil {
			// The error came from parsing flags, before the logger was set up.
			logger = slog.New(slog.NewTextHandler(stderr, nil))
		}
		logger.Error(err.Error())
	}
	return err
}

func newRootCmd(a *app) *cobra.Command {
	root := &cobra.Command{
		Use:   "jmdict-to-json",
		Short: "Download the JMdict dictionary and convert it to JSON",
		Long: `Download the JMdict dictionary and convert it to JSON.

Files may be local paths, "-" for stdin or stdout, or gs://bucket/object.

JMdict is the property of the Electronic Dictionary Research and Development
Group, and is used in conformance with the Group's licence (CC BY-SA 4.0).
See https://www.edrdg.org/edrdg/licence.html.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			logger, err := logging.New(cmd.ErrOrStderr(), a.logFormat)
			if err != nil {
				return err
			}
			a.logger = logger
			a.store = storage.NewRouter(cmd.InOrStdin(), cmd.OutOrStdout())
			return nil
		},
	}

	defaultFormat := os.Getenv("LOG_FORMAT")
	if defaultFormat == "" {
		defaultFormat = logging.FormatText
	}
	root.PersistentFlags().StringVar(&a.logFormat, "log-format", defaultFormat,
		"log format, text or json (env LOG_FORMAT)")

	root.AddCommand(newDownloadCmd(a), newUnzipCmd(a), newConvertCmd(a))
	return root
}
