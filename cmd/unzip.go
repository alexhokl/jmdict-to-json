package cmd

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/alexhokl/jmdict-to-json/internal/storage"
)

type unzipOptions struct {
	input  string
	output string
	force  bool
}

func newUnzipCmd(a *app) *cobra.Command {
	opts := &unzipOptions{}
	cmd := &cobra.Command{
		Use:   "unzip",
		Short: "Decompress the JMdict .gz file to XML",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.unzip(cmd, opts)
		},
	}
	cmd.Flags().StringVarP(&opts.input, "input", "i", "jmdict.gz", "input .gz file")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "jmdict.xml", "output XML file")
	cmd.Flags().BoolVar(&opts.force, "force", false, "overwrite the output if it exists")
	return cmd
}

func (a *app) unzip(cmd *cobra.Command, opts *unzipOptions) error {
	ctx := cmd.Context()

	in, err := a.store.Open(ctx, opts.input)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	gz, err := gzip.NewReader(bufio.NewReader(in))
	if err != nil {
		return fmt.Errorf("read %s: %w", opts.input, err)
	}

	w, err := a.store.Create(ctx, opts.output, storage.CreateOptions{Force: opts.force})
	if err != nil {
		return err
	}
	defer w.Abort()

	n, err := io.Copy(w, gz)
	if err != nil {
		return fmt.Errorf("decompress %s: %w", opts.input, err)
	}
	if err := w.Commit(); err != nil {
		return err
	}
	a.logger.Info("unzipped", "input", opts.input, "output", opts.output, "bytes", n)
	return nil
}
