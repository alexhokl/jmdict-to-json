# jmdict-to-json

Download the [JMdict](https://www.edrdg.org/jmdict/j_jmdict.html) Japanese
dictionary and convert its XML to JSON.

## Install

```sh
go install github.com/alexhokl/jmdict-to-json@latest
```

## Usage

Files may be local paths, `-` for stdin or stdout, or `gs://bucket/object`
(using Application Default Credentials).

### Download

```sh
jmdict-to-json download
```

Downloads `JMdict_e.gz` (English only) to `jmdict.gz`. When `jmdict.gz` exists
it is downloaded again only if the server copy is newer; `--force` downloads
regardless. Use `--url` for another variant, such as
`http://ftp.edrdg.org/pub/Nihongo/JMdict.gz` (multilingual).

### Unzip

```sh
jmdict-to-json unzip              # jmdict.gz -> jmdict.xml
```

### Convert

```sh
jmdict-to-json convert                                  # jmdict.xml -> jmdict.json
jmdict-to-json convert -i jmdict.gz -o - | jq '.[0]'    # .gz is detected
jmdict-to-json convert --jsonl -o jmdict.jsonl          # one entry per line
jmdict-to-json convert --pretty --expand-entities
```

The output is a single JSON array; `--pretty` indents it and `--jsonl` writes
JSON Lines instead. Entity values are kept as codes (`"n"`) unless
`--expand-entities` is given (`"noun (common) (futsuumeishi)"`).

`unzip` and `convert` fail if the output exists, unless `--force` is given.

`--log-format json` (or `LOG_FORMAT=json`) logs in the structured format of
Cloud Logging.

## JSON format

Every XML element is kept, under camelCase names. Empty fields are omitted.

```json
{
  "id": 1000220,
  "kanji": [{ "text": "明白", "priority": ["ichi1", "news1", "nf10"] }],
  "kana": [{ "text": "めいはく", "priority": ["ichi1", "news1", "nf10"] }],
  "sense": [
    {
      "partOfSpeech": ["adj-na"],
      "gloss": [{ "lang": "eng", "text": "obvious" }]
    }
  ]
}
```

| JSON                                  | XML                                         |
| ------------------------------------- | ------------------------------------------- |
| `id`                                  | `ent_seq`                                   |
| `kanji[].text` / `info` / `priority`  | `keb` / `ke_inf` / `ke_pri`                 |
| `kana[].text` / `noKanji`             | `reb` / `re_nokanji`                        |
| `kana[].appliesToKanji` / `info` / `priority` | `re_restr` / `re_inf` / `re_pri`    |
| `sense[].appliesToKanji` / `appliesToKana` | `stagk` / `stagr`                      |
| `sense[].partOfSpeech` / `xref` / `antonym` | `pos` / `xref` / `ant`                |
| `sense[].field` / `misc` / `info` / `dialect` | `field` / `misc` / `s_inf` / `dial` |
| `sense[].languageSource[]`            | `lsource` (`full` is `ls_type` not `part`, `wasei` is `ls_wasei="y"`) |
| `sense[].gloss[]`                     | `gloss` (`lang`, `gender`, `type`, `text`, `priority`) |
| `sense[].example[]`                   | `example` (only in `JMdict_e_examp`)        |

## Agent skill

[`skills/jmdict-to-json/`](skills/jmdict-to-json) is an Agent Skill that
teaches a coding agent to drive the `jmdict-to-json` CLI and query its JSON.
Install it into `~/.agents/skills` and `~/.claude/skills` with
`task install-skill`.

## Data licence and attribution

This package uses the
[JMdict](https://www.edrdg.org/wiki/index.php/JMdict-EDICT_Dictionary_Project)
dictionary file. This file is the property of the
[Electronic Dictionary Research and Development Group](https://www.edrdg.org/),
and is used in conformance with the Group's
[licence](https://www.edrdg.org/edrdg/licence.html).

- The source code of this program is licensed under MIT (see `LICENSE`). The
  MIT licence does not apply to the data: JMdict and the JSON produced from it
  are licensed under
  [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/).
- The JSON is a modified form of JMdict: the XML is converted to JSON with the
  field names above, and entity references are kept as codes or expanded with
  `--expand-entities`. No entries or text are otherwise altered.
- This project is not affiliated with or endorsed by EDRDG.
- EDRDG updates JMdict almost daily, and the licence requires users of the data
  to update it regularly. Re-run `download` and `convert` regularly; the Cloud
  Run job below does so daily.
- Anything that displays words from the JSON needs its own acknowledgement: on
  each page of a web dictionary, or on an "About" or "Sources" screen of an
  app.

## Cloud Run

The Docker image runs the CLI. The Cloud Run job defined in the `cluster`
repository runs `/job.sh` daily instead, which downloads to and converts in
the bucket named by `BUCKET`.

A tag `0.1.x` (`task tag`) builds and pushes the image with Cloud Build; the
image tag of the job is then bumped by hand in `cluster`.
