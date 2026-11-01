// Package jmdict converts the JMdict XML dictionary to JSON.
//
// See https://www.edrdg.org/jmdict/jmdict_dtd_h.html for the meaning of the
// XML elements.
package jmdict

// defaultLang is the language the DTD assumes when xml:lang is absent.
const defaultLang = "eng"

// Entry is the JSON form of a JMdict <entry>.
type Entry struct {
	ID    int     `json:"id"`
	Kanji []Kanji `json:"kanji,omitempty"`
	Kana  []Kana  `json:"kana,omitempty"`
	Sense []Sense `json:"sense,omitempty"`
}

// Kanji is the JSON form of <k_ele>.
type Kanji struct {
	Text     string   `json:"text"`
	Info     []string `json:"info,omitempty"`
	Priority []string `json:"priority,omitempty"`
}

// Kana is the JSON form of <r_ele>.
type Kana struct {
	Text           string   `json:"text"`
	NoKanji        bool     `json:"noKanji,omitempty"`
	AppliesToKanji []string `json:"appliesToKanji,omitempty"`
	Info           []string `json:"info,omitempty"`
	Priority       []string `json:"priority,omitempty"`
}

// Sense is the JSON form of <sense>.
type Sense struct {
	AppliesToKanji []string         `json:"appliesToKanji,omitempty"`
	AppliesToKana  []string         `json:"appliesToKana,omitempty"`
	PartOfSpeech   []string         `json:"partOfSpeech,omitempty"`
	Xref           []string         `json:"xref,omitempty"`
	Antonym        []string         `json:"antonym,omitempty"`
	Field          []string         `json:"field,omitempty"`
	Misc           []string         `json:"misc,omitempty"`
	Info           []string         `json:"info,omitempty"`
	LanguageSource []LanguageSource `json:"languageSource,omitempty"`
	Dialect        []string         `json:"dialect,omitempty"`
	Gloss          []Gloss          `json:"gloss,omitempty"`
	Example        []Example        `json:"example,omitempty"`
}

// LanguageSource is the JSON form of <lsource>.
type LanguageSource struct {
	Lang  string `json:"lang"`
	Text  string `json:"text,omitempty"`
	Full  bool   `json:"full,omitempty"`
	Wasei bool   `json:"wasei,omitempty"`
}

// Gloss is the JSON form of <gloss>.
type Gloss struct {
	Lang     string   `json:"lang"`
	Gender   string   `json:"gender,omitempty"`
	Type     string   `json:"type,omitempty"`
	Text     string   `json:"text"`
	Priority []string `json:"priority,omitempty"`
}

// Example is the JSON form of <example>, present only in JMdict_e_examp.
type Example struct {
	Source    ExampleSource     `json:"source"`
	Text      string            `json:"text"`
	Sentences []ExampleSentence `json:"sentences,omitempty"`
}

// ExampleSource is the JSON form of <ex_srce>.
type ExampleSource struct {
	Type string `json:"type,omitempty"`
	Text string `json:"text"`
}

// ExampleSentence is the JSON form of <ex_sent>.
type ExampleSentence struct {
	Lang string `json:"lang"`
	Text string `json:"text"`
}

// The xml* types mirror the DTD. They are kept apart from the JSON types
// because several values need converting, such as the presence of
// <re_nokanji/> into a bool and the implied xml:lang default.

type xmlEntry struct {
	Seq   int        `xml:"ent_seq"`
	Kanji []xmlKanji `xml:"k_ele"`
	Kana  []xmlKana  `xml:"r_ele"`
	Sense []xmlSense `xml:"sense"`
}

type xmlKanji struct {
	Text     string   `xml:"keb"`
	Info     []string `xml:"ke_inf"`
	Priority []string `xml:"ke_pri"`
}

type xmlKana struct {
	Text     string   `xml:"reb"`
	NoKanji  *string  `xml:"re_nokanji"`
	Restr    []string `xml:"re_restr"`
	Info     []string `xml:"re_inf"`
	Priority []string `xml:"re_pri"`
}

type xmlSense struct {
	Stagk   []string     `xml:"stagk"`
	Stagr   []string     `xml:"stagr"`
	Pos     []string     `xml:"pos"`
	Xref    []string     `xml:"xref"`
	Ant     []string     `xml:"ant"`
	Field   []string     `xml:"field"`
	Misc    []string     `xml:"misc"`
	Info    []string     `xml:"s_inf"`
	Lsource []xmlLsource `xml:"lsource"`
	Dial    []string     `xml:"dial"`
	Gloss   []xmlGloss   `xml:"gloss"`
	Example []xmlExample `xml:"example"`
}

type xmlLsource struct {
	Lang  string `xml:"lang,attr"`
	Type  string `xml:"ls_type,attr"`
	Wasei string `xml:"ls_wasei,attr"`
	Text  string `xml:",chardata"`
}

type xmlGloss struct {
	Lang   string   `xml:"lang,attr"`
	Gender string   `xml:"g_gend,attr"`
	Type   string   `xml:"g_type,attr"`
	Text   string   `xml:",chardata"`
	Pri    []string `xml:"pri"`
}

type xmlExample struct {
	Source struct {
		Type string `xml:"exsrc_type,attr"`
		Text string `xml:",chardata"`
	} `xml:"ex_srce"`
	Text      string `xml:"ex_text"`
	Sentences []struct {
		Lang string `xml:"lang,attr"`
		Text string `xml:",chardata"`
	} `xml:"ex_sent"`
}

func (x *xmlEntry) toEntry() Entry {
	e := Entry{ID: x.Seq}
	for _, k := range x.Kanji {
		e.Kanji = append(e.Kanji, Kanji(k))
	}
	for _, r := range x.Kana {
		e.Kana = append(e.Kana, Kana{
			Text:           r.Text,
			NoKanji:        r.NoKanji != nil,
			AppliesToKanji: r.Restr,
			Info:           r.Info,
			Priority:       r.Priority,
		})
	}
	for _, s := range x.Sense {
		e.Sense = append(e.Sense, s.toSense())
	}
	return e
}

func (s *xmlSense) toSense() Sense {
	out := Sense{
		AppliesToKanji: s.Stagk,
		AppliesToKana:  s.Stagr,
		PartOfSpeech:   s.Pos,
		Xref:           s.Xref,
		Antonym:        s.Ant,
		Field:          s.Field,
		Misc:           s.Misc,
		Info:           s.Info,
		Dialect:        s.Dial,
	}
	for _, l := range s.Lsource {
		out.LanguageSource = append(out.LanguageSource, LanguageSource{
			Lang:  langOrDefault(l.Lang),
			Text:  l.Text,
			Full:  l.Type != "part",
			Wasei: l.Wasei == "y",
		})
	}
	for _, g := range s.Gloss {
		out.Gloss = append(out.Gloss, Gloss{
			Lang:     langOrDefault(g.Lang),
			Gender:   g.Gender,
			Type:     g.Type,
			Text:     g.Text,
			Priority: g.Pri,
		})
	}
	for _, ex := range s.Example {
		example := Example{
			Source: ExampleSource{Type: ex.Source.Type, Text: ex.Source.Text},
			Text:   ex.Text,
		}
		for _, sent := range ex.Sentences {
			example.Sentences = append(example.Sentences, ExampleSentence{
				Lang: langOrDefault(sent.Lang),
				Text: sent.Text,
			})
		}
		out.Example = append(out.Example, example)
	}
	return out
}

func langOrDefault(lang string) string {
	if lang == "" {
		return defaultLang
	}
	return lang
}
