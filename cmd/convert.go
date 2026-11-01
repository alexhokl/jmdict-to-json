package cmd

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/alexhokl/jmdict-to-json/internal/jmdict"
	"github.com/alexhokl/jmdict-to-json/internal/storage"
)

type convertOptions struct {
	input          string
	output         string
	force          bool
	pretty         bool
	jsonl          bool
	expandEntities bool
}

func newConvertCmd(a *app) *cobra.Command {
	opts := &convertOptions{}
	cmd := &cobra.Command{
		Use:   "convert",
		Short: "Convert JMdict XML to JSON",
		Long: `Convert JMdict XML to JSON.

The input may be the XML file or the .gz file, which is detected
automatically. The output is a single JSON array unless --jsonl is given.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.convert(cmd, opts)
		},
	}
	cmd.Flags().StringVarP(&opts.input, "input", "i", "jmdict.xml", "input XML or .gz file")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "jmdict.json", "output JSON file")
	cmd.Flags().BoolVar(&opts.force, "force", false, "overwrite the output if it exists")
	cmd.Flags().BoolVar(&opts.pretty, "pretty", false, "indent the JSON array")
	cmd.Flags().BoolVar(&opts.jsonl, "jsonl", false, "write JSON Lines, one entry per line")
	cmd.Flags().BoolVar(&opts.expandEntities, "expand-entities", false,
		`output entity descriptions such as "noun (common) (futsuumeishi)" instead of codes such as "n"`)
	cmd.MarkFlagsMutuallyExclusive("pretty", "jsonl")
	return cmd
}

func (a *app) convert(cmd *cobra.Command, opts *convertOptions) error {
	ctx := cmd.Context()

	in, err := a.store.Open(ctx, opts.input)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	xmlReader, err := maybeGunzip(in)
	if err != nil {
		return fmt.Errorf("read %s: %w", opts.input, err)
	}

	w, err := a.store.Create(ctx, opts.output, storage.CreateOptions{Force: opts.force})
	if err != nil {
		return err
	}
	defer w.Abort()

	format := jmdict.Array
	switch {
	case opts.pretty:
		format = jmdict.Pretty
	case opts.jsonl:
		format = jmdict.JSONLines
	}
	n, err := jmdict.Convert(xmlReader, w, jmdict.Options{Format: format, ExpandEntities: opts.expandEntities})
	if err != nil {
		return fmt.Errorf("convert %s: %w", opts.input, err)
	}
	if err := w.Commit(); err != nil {
		return err
	}
	a.logger.Info("converted", "input", opts.input, "output", opts.output, "entries", n)
	return nil
}

var gzipMagic = []byte{0x1f, 0x8b}

// maybeGunzip decompresses r if it starts with the gzip magic bytes.
func maybeGunzip(r io.Reader) (io.Reader, error) {
	br := bufio.NewReader(r)
	magic, err := br.Peek(len(gzipMagic))
	if err != nil && err != io.EOF {
		return nil, err
	}
	if bytes.Equal(magic, gzipMagic) {
		return gzip.NewReader(br)
	}
	return br, nil
}
