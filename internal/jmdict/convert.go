package jmdict

import (
	"bufio"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
)

// Format is the layout of the JSON output.
type Format int

const (
	// Array writes a single compact JSON array.
	Array Format = iota
	// Pretty writes a single indented JSON array.
	Pretty
	// JSONLines writes one compact JSON object per line.
	JSONLines
)

// Options controls Convert.
type Options struct {
	Format Format
	// ExpandEntities replaces entity codes such as "n" with their
	// descriptions such as "noun (common) (futsuumeishi)".
	ExpandEntities bool
}

// entityPattern matches the entity declarations in the DOCTYPE internal
// subset, e.g. <!ENTITY n "noun (common) (futsuumeishi)">.
var entityPattern = regexp.MustCompile(`<!ENTITY\s+([^\s%"']+)\s+(?:"([^"]*)"|'([^']*)')\s*>`)

// ParseEntities returns the general entities declared in a DOCTYPE
// directive, keyed by name.
func ParseEntities(directive []byte) map[string]string {
	entities := make(map[string]string)
	for _, m := range entityPattern.FindAllSubmatch(directive, -1) {
		value := m[2]
		if value == nil {
			value = m[3]
		}
		entities[string(m[1])] = string(value)
	}
	return entities
}

// Convert reads JMdict XML from r and writes its entries as JSON to w,
// one entry at a time so the whole dictionary is never held in memory. It
// returns the number of entries written.
//
// The entity definitions are taken from the DOCTYPE of the input rather
// than hard-coded, so entities added to JMdict later are handled.
func Convert(r io.Reader, w io.Writer, opts Options) (int, error) {
	bw := bufio.NewWriter(w)
	enc := newEntryWriter(bw, opts.Format)

	d := xml.NewDecoder(bufio.NewReader(r))
	count := 0
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return count, fmt.Errorf("read XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.Directive:
			if bytes.HasPrefix(t, []byte("DOCTYPE")) {
				d.Entity = entityMap(ParseEntities(t), opts.ExpandEntities)
			}
		case xml.StartElement:
			if t.Name.Local != "entry" {
				continue
			}
			var x xmlEntry
			if err := d.DecodeElement(&x, &t); err != nil {
				return count, fmt.Errorf("decode entry after %d entries: %w", count, err)
			}
			if err := enc.write(x.toEntry()); err != nil {
				return count, err
			}
			count++
		}
	}
	if err := enc.close(); err != nil {
		return count, err
	}
	return count, bw.Flush()
}

// entityMap returns the replacement text the XML decoder uses for each
// entity: the entity's own name to keep codes, or its description.
func entityMap(entities map[string]string, expand bool) map[string]string {
	if expand {
		return entities
	}
	codes := make(map[string]string, len(entities))
	for name := range entities {
		codes[name] = name
	}
	return codes
}

type entryWriter struct {
	w      io.Writer
	format Format
	buf    bytes.Buffer
	enc    *json.Encoder
	n      int
}

func newEntryWriter(w io.Writer, format Format) *entryWriter {
	ew := &entryWriter{w: w, format: format}
	ew.enc = json.NewEncoder(&ew.buf)
	ew.enc.SetEscapeHTML(false)
	if format == Pretty {
		ew.enc.SetIndent("  ", "  ")
	}
	return ew
}

func (ew *entryWriter) write(e Entry) error {
	ew.buf.Reset()
	switch {
	case ew.format == JSONLines:
	case ew.n == 0 && ew.format == Pretty:
		ew.buf.WriteString("[\n  ")
	case ew.n == 0:
		ew.buf.WriteString("[")
	case ew.format == Pretty:
		ew.buf.WriteString(",\n  ")
	default:
		ew.buf.WriteString(",")
	}
	if err := ew.enc.Encode(e); err != nil {
		return fmt.Errorf("encode entry %d: %w", e.ID, err)
	}
	out := ew.buf.Bytes()
	if ew.format != JSONLines {
		// Encode always appends a newline; the array separators take its place.
		out = bytes.TrimSuffix(out, []byte("\n"))
	}
	ew.n++
	_, err := ew.w.Write(out)
	return err
}

func (ew *entryWriter) close() error {
	var tail string
	switch {
	case ew.format == JSONLines:
		return nil
	case ew.n == 0:
		tail = "[]\n"
	case ew.format == Pretty:
		tail = "\n]\n"
	default:
		tail = "]\n"
	}
	_, err := io.WriteString(ew.w, tail)
	return err
}
