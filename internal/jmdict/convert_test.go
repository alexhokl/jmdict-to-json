package jmdict

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func convertSample(t *testing.T, opts Options) (string, int) {
	t.Helper()
	f, err := os.Open("testdata/sample.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out bytes.Buffer
	n, err := Convert(f, &out, opts)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	return out.String(), n
}

func TestParseEntities(t *testing.T) {
	directive := []byte(`DOCTYPE JMdict [
<!ENTITY n "noun (common) (futsuumeishi)">
<!ENTITY ksb 'Kansai-ben'>
<!ENTITY % param "ignored">
]`)
	got := ParseEntities(directive)
	want := map[string]string{
		"n":   "noun (common) (futsuumeishi)",
		"ksb": "Kansai-ben",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseEntities = %v, want %v", got, want)
	}
}

var wantEntries = []Entry{
	{
		ID:    1000220,
		Kanji: []Kanji{{Text: "明白", Info: []string{"ateji"}, Priority: []string{"ichi1", "news1"}}},
		Kana:  []Kana{{Text: "めいはく", Priority: []string{"ichi1"}}},
		Sense: []Sense{
			{
				PartOfSpeech: []string{"adj-na"},
				Gloss: []Gloss{
					{Lang: "eng", Text: "obvious"},
					{Lang: "eng", Type: "lit", Text: "clear & plain"},
				},
			},
			{
				PartOfSpeech: []string{"n"},
				Misc:         []string{"uk"},
				Dialect:      []string{"ksb"},
				LanguageSource: []LanguageSource{
					{Lang: "por", Text: "espada", Full: false, Wasei: true},
					{Lang: "eng", Full: true},
				},
				Gloss: []Gloss{{Lang: "ger", Gender: "fem", Text: "Klarheit"}},
			},
		},
	},
	{
		ID:   2000010,
		Kana: []Kana{{Text: "ワイシャツ", NoKanji: true, AppliesToKanji: []string{"白"}}},
		Sense: []Sense{{
			AppliesToKana: []string{"ワイシャツ"},
			Xref:          []string{"シャツ・1"},
			Gloss:         []Gloss{{Lang: "eng", Text: "shirt"}},
			Example: []Example{{
				Source: ExampleSource{Type: "tat", Text: "12345"},
				Text:   "ワイシャツ",
				Sentences: []ExampleSentence{
					{Lang: "jpn", Text: "ワイシャツを着る。"},
					{Lang: "eng", Text: "I wear a shirt."},
				},
			}},
		}},
	},
}

func TestConvertArray(t *testing.T) {
	out, n := convertSample(t, Options{})
	if n != 2 {
		t.Errorf("count = %d, want 2", n)
	}
	if strings.Count(out, "\n") != 1 || !strings.HasPrefix(out, "[{") || !strings.HasSuffix(out, "}]\n") {
		t.Errorf("output is not a compact array:\n%s", out)
	}
	var got []Entry
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, wantEntries) {
		t.Errorf("entries =\n%+v\nwant\n%+v", got, wantEntries)
	}
}

func TestConvertOmitsEmptyFields(t *testing.T) {
	out, _ := convertSample(t, Options{})
	for _, field := range []string{`"field"`, `"antonym"`, `null`, `false`, `"noKanji":false`} {
		if strings.Contains(out, field) {
			t.Errorf("output contains %s:\n%s", field, out)
		}
	}
	if !strings.Contains(out, `"noKanji":true`) {
		t.Errorf("output lacks noKanji:true:\n%s", out)
	}
}

func TestConvertPretty(t *testing.T) {
	out, _ := convertSample(t, Options{Format: Pretty})
	if !strings.HasPrefix(out, "[\n  {\n    \"id\": 1000220,") || !strings.HasSuffix(out, "\n  }\n]\n") {
		t.Errorf("output is not an indented array:\n%s", out)
	}
	var got []Entry
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, wantEntries) {
		t.Errorf("entries differ from the compact form")
	}
}

func TestConvertJSONLines(t *testing.T) {
	out, _ := convertSample(t, Options{Format: JSONLines})
	var got []Entry
	s := bufio.NewScanner(strings.NewReader(out))
	for s.Scan() {
		var e Entry
		if err := json.Unmarshal(s.Bytes(), &e); err != nil {
			t.Fatalf("line %q: %v", s.Text(), err)
		}
		got = append(got, e)
	}
	if !reflect.DeepEqual(got, wantEntries) {
		t.Errorf("entries =\n%+v\nwant\n%+v", got, wantEntries)
	}
}

func TestConvertExpandEntities(t *testing.T) {
	out, _ := convertSample(t, Options{ExpandEntities: true})
	var got []Entry
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if pos := got[0].Sense[1].PartOfSpeech[0]; pos != "noun (common) (futsuumeishi)" {
		t.Errorf("partOfSpeech = %q", pos)
	}
	if dial := got[0].Sense[1].Dialect[0]; dial != "Kansai-ben" {
		t.Errorf("dialect = %q", dial)
	}
}

func TestConvertEmpty(t *testing.T) {
	for format, want := range map[Format]string{Array: "[]\n", Pretty: "[]\n", JSONLines: ""} {
		var out bytes.Buffer
		n, err := Convert(strings.NewReader("<JMdict></JMdict>"), &out, Options{Format: format})
		if err != nil || n != 0 || out.String() != want {
			t.Errorf("format %d: got %q, %d, %v; want %q", format, out.String(), n, err, want)
		}
	}
}

func TestConvertUndefinedEntity(t *testing.T) {
	in := "<JMdict><entry><ent_seq>1</ent_seq><sense><pos>&n;</pos></sense></entry></JMdict>"
	if _, err := Convert(strings.NewReader(in), &bytes.Buffer{}, Options{}); err == nil {
		t.Error("expected an error for an undeclared entity")
	}
}

func TestConvertInvalidXML(t *testing.T) {
	tests := map[string]string{
		"malformed":      "<JMdict><entry>",
		"bad entry type": "<JMdict><entry><ent_seq>abc</ent_seq></entry></JMdict>",
	}
	for name, in := range tests {
		if _, err := Convert(strings.NewReader(in), &bytes.Buffer{}, Options{}); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

type failingWriter struct{ after int }

func (w *failingWriter) Write(p []byte) (int, error) {
	if w.after <= 0 {
		return 0, errors.New("disk full")
	}
	w.after--
	return len(p), nil
}

func TestConvertWriteError(t *testing.T) {
	f, err := os.Open("testdata/sample.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	// No buffering below Convert's own, so the error surfaces at the flush.
	if _, err := Convert(f, &failingWriter{}, Options{}); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("err = %v, want disk full", err)
	}
}

func TestEntryWriterErrors(t *testing.T) {
	ew := newEntryWriter(&failingWriter{}, JSONLines)
	if err := ew.write(Entry{ID: 1}); err == nil {
		t.Error("write: expected an error")
	}
	ew = newEntryWriter(&failingWriter{}, Array)
	if err := ew.close(); err == nil {
		t.Error("close: expected an error")
	}
}

func TestConvertSkipsOtherElements(t *testing.T) {
	in := `<?xml version="1.0"?><!-- c --><JMdict><other/><entry><ent_seq>7</ent_seq></entry></JMdict>`
	var out bytes.Buffer
	n, err := Convert(strings.NewReader(in), &out, Options{})
	if err != nil || n != 1 || out.String() != "[{\"id\":7}]\n" {
		t.Errorf("got %q, %d, %v", out.String(), n, err)
	}
}
