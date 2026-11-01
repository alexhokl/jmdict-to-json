package cmd

import (
	"errors"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/alexhokl/jmdict-to-json/internal/fetch"
	"github.com/alexhokl/jmdict-to-json/internal/logging"
	"github.com/alexhokl/jmdict-to-json/internal/storage"
)

// defaultURL is served over HTTP as Go has no FTP client and EDRDG does
// not serve HTTPS.
const defaultURL = "http://ftp.edrdg.org/pub/Nihongo/JMdict_e.gz"

type downloadOptions struct {
	url    string
	output string
	force  bool
}

func newDownloadCmd(a *app) *cobra.Command {
	opts := &downloadOptions{}
	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download the JMdict .gz file",
		Long: `Download the JMdict .gz file.

If the output already exists, it is downloaded again only if the server copy
is newer, unless --force is given.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.download(cmd, opts)
		},
	}
	cmd.Flags().StringVar(&opts.url, "url", defaultURL, "URL of the JMdict .gz file")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "jmdict.gz", "output file")
	cmd.Flags().BoolVar(&opts.force, "force", false, "download even if the output is up to date")
	return cmd
}

func (a *app) download(cmd *cobra.Command, opts *downloadOptions) error {
	ctx := cmd.Context()

	var since time.Time
	if !opts.force {
		lastModified, err := a.store.LastModified(ctx, opts.output)
		switch {
		case err == nil:
			since = lastModified
		case !errors.Is(err, storage.ErrNotExist):
			return err
		}
	}

	resp, err := fetch.Get(ctx, a.httpClient, opts.url, since)
	if err != nil {
		return err
	}
	if resp == nil {
		a.logger.Info("already up to date", "output", opts.output, "last_modified", since)
		return nil
	}
	defer func() { _ = resp.Body.Close() }()

	// The output is replaced even without --force, as keeping it up to date
	// is what download is for.
	w, err := a.store.Create(ctx, opts.output, storage.CreateOptions{
		Force:        true,
		LastModified: resp.LastModified,
	})
	if err != nil {
		return err
	}
	defer w.Abort()

	var dst io.Writer = w
	var progress *fetch.Progress
	if a.logFormat == logging.FormatText {
		progress = &fetch.Progress{Out: cmd.ErrOrStderr(), Total: resp.ContentLength, Interval: time.Second}
		dst = io.MultiWriter(w, progress)
	}
	n, err := io.Copy(dst, resp.Body)
	if progress != nil {
		progress.Done()
	}
	if err != nil {
		return err
	}
	if err := w.Commit(); err != nil {
		return err
	}
	a.logger.Info("downloaded", "output", opts.output, "bytes", n, "last_modified", resp.LastModified)
	return nil
}
