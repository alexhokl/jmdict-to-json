---
name: jmdict-to-json
description: Use when the user asks to download the JMdict Japanese dictionary, unzip it, convert it to JSON or JSON Lines, or look up or query JMdict entries using the jmdict-to-json CLI. Trigger on "JMdict", "jmdict.json", "JMdict_e", "EDRDG", "convert JMdict to JSON", or a request to look up a Japanese word in JMdict.
---

# jmdict-to-json

Downloads the JMdict dictionary (`JMdict_e.gz`, English glosses) from EDRDG,
decompresses it, and converts its XML to JSON with three subcommands:
`download`, `unzip` and `convert`. There is no all-in-one command; chain them.

## Plan / Ask Mode (Read-Only)

Before executing any command, check whether a `<system-reminder>` block containing
`Plan mode ACTIVE` is present in your current context. If it is, you are in
**read-only mode** and MUST NOT write any files to disk or to Cloud Storage.

**Forbidden in read-only mode:**
- `jmdict-to-json download ...` (writes the `.gz` file)
- `jmdict-to-json unzip ...` (writes the XML file)
- `jmdict-to-json convert ...` with `-o` set to a file or `gs://` path
- Redirecting any output into a file (`> file`)

**Allowed in read-only mode:**
- `jmdict-to-json --help`, `jmdict-to-json <command> --help`
- `which jmdict-to-json`
- `jmdict-to-json convert -i <existing file> -o -` piped to `jq`, `grep` or `head`
  (reads only)
- `jq` or `grep` over an existing `jmdict.json` or `.jsonl` file

If the user requests a write while in read-only mode, respond:
> "I am currently in plan/read-only mode. I can describe what this operation would do,
> but I will not write files until plan mode ends."

## Prerequisites

Verify the binary is installed before invoking it:

```bash
which jmdict-to-json
```

If not found, install it:

```bash
go install github.com/alexhokl/jmdict-to-json@latest
```

Or, from the repository root:

```bash
task install
```

`gs://` paths use Google Application Default Credentials
(`gcloud auth application-default login`).

## Paths

Every `--input` / `--output` accepts:

| Form                  | Meaning                     |
|-----------------------|-----------------------------|
| `jmdict.gz`           | Local file                  |
| `-`                   | stdin (input) or stdout (output) |
| `gs://bucket/object`  | Google Cloud Storage object |

## Flag Reference

Global flag, valid on every command:

| Flag           | Default                    | Description                                  |
|----------------|----------------------------|----------------------------------------------|
| `--log-format` | `text` (env `LOG_FORMAT`)  | `text`, or `json` for Cloud Logging format   |

### `download`

| Flag       | Short | Default                                           | Description                              |
|------------|-------|---------------------------------------------------|------------------------------------------|
| `--url`    | —     | `http://ftp.edrdg.org/pub/Nihongo/JMdict_e.gz`    | URL of the JMdict `.gz` file             |
| `--output` | `-o`  | `jmdict.gz`                                       | Output file                              |
| `--force`  | —     | `false`                                           | Download even if the output is up to date |

### `unzip`

| Flag       | Short | Default      | Description                        |
|------------|-------|--------------|------------------------------------|
| `--input`  | `-i`  | `jmdict.gz`  | Input `.gz` file                   |
| `--output` | `-o`  | `jmdict.xml` | Output XML file                    |
| `--force`  | —     | `false`      | Overwrite the output if it exists  |

### `convert`

| Flag                | Short | Default       | Description                                             |
|---------------------|-------|---------------|---------------------------------------------------------|
| `--input`           | `-i`  | `jmdict.xml`  | Input XML or `.gz` file (gzip is detected automatically) |
| `--output`          | `-o`  | `jmdict.json` | Output JSON file                                        |
| `--force`           | —     | `false`       | Overwrite the output if it exists                       |
| `--pretty`          | —     | `false`       | Indent the JSON array                                   |
| `--jsonl`           | —     | `false`       | Write JSON Lines, one entry per line                    |
| `--expand-entities` | —     | `false`       | Output entity descriptions instead of codes             |

`--pretty` and `--jsonl` cannot be used together.

## Key Commands

### 1. First run: download and convert

```bash
jmdict-to-json download
jmdict-to-json convert -i jmdict.gz
```

`convert` reads the `.gz` directly; `unzip` is only needed when the XML itself
is wanted.

### 2. Refresh an existing copy

```bash
jmdict-to-json download                          # no-op if EDRDG has nothing newer
jmdict-to-json convert -i jmdict.gz --force      # --force replaces jmdict.json
```

### 3. JSON Lines for searching

```bash
jmdict-to-json convert -i jmdict.gz -o jmdict.jsonl --jsonl
```

### 4. Readable output with entity descriptions

```bash
jmdict-to-json convert -i jmdict.gz -o jmdict.pretty.json --pretty --expand-entities
```

### 5. Decompress to XML

```bash
jmdict-to-json unzip                             # jmdict.gz -> jmdict.xml
```

### 6. Cloud Storage

```bash
jmdict-to-json download -o gs://my-bucket/jmdict.gz
jmdict-to-json convert -i gs://my-bucket/jmdict.gz -o gs://my-bucket/jmdict.json --force
```

### 7. Use the published copy instead of converting

A daily-refreshed conversion is public:

```bash
curl -sO https://storage.googleapis.com/alexhokl_jmdict_to_json/jmdict.json
```

## Looking Up Words

Prefer JSON Lines with `jq` (one entry per line, streams without loading the
whole 56 MB array). Arrays that are empty are omitted, so use `[]?` on
`kanji`, which kana-only words lack.

```bash
# By kanji
jq -c 'select(any(.kanji[]?; .text == "猫"))' jmdict.jsonl

# By reading, showing kanji and English glosses
jq -c 'select(any(.kana[]; .text == "ねこ"))
       | {id, kanji: [.kanji[]?.text], gloss: [.sense[].gloss[].text]}' jmdict.jsonl

# By English gloss
jq -c 'select(any(.sense[].gloss[]; .text == "cat"))
       | {id, kanji: [.kanji[]?.text], kana: [.kana[].text]}' jmdict.jsonl

# By entry ID (ent_seq), on the array form
jq '.[] | select(.id == 1000220)' jmdict.json

# Without an intermediate file
jmdict-to-json convert -i jmdict.gz -o - --jsonl | jq -c 'select(.id == 1000220)'
```

For a quick pre-filter, `grep '"text":"猫"' jmdict.jsonl` is much faster than
`jq` and then `jq` can refine the matched lines.

## JSON Format

Each entry (fields with no value are omitted):

| Field                          | From XML     | Notes                                        |
|--------------------------------|--------------|----------------------------------------------|
| `id`                           | `ent_seq`    | Number                                       |
| `kanji[].text`                 | `keb`        | Absent for kana-only words                   |
| `kanji[].info` / `priority`    | `ke_inf` / `ke_pri` |                                       |
| `kana[].text`                  | `reb`        |                                              |
| `kana[].noKanji`               | `re_nokanji` | `true` when the reading is not a true reading of the kanji |
| `kana[].appliesToKanji`        | `re_restr`   |                                              |
| `kana[].info` / `priority`     | `re_inf` / `re_pri` |                                       |
| `sense[].partOfSpeech`         | `pos`        |                                              |
| `sense[].gloss[]`              | `gloss`      | `lang` (default `eng`), `text`, optional `type`, `gender` |
| `sense[].appliesToKanji` / `appliesToKana` | `stagk` / `stagr` |                              |
| `sense[].xref` / `antonym`     | `xref` / `ant` |                                            |
| `sense[].field` / `misc` / `dialect` / `info` | `field` / `misc` / `dial` / `s_inf` |      |
| `sense[].languageSource[]`     | `lsource`    | `lang`, `text`, `full`, `wasei`              |
| `sense[].example[]`            | `example`    | Only in the `JMdict_e_examp` variant         |

Entity values (`partOfSpeech`, `misc`, `field`, `dialect`, `info`) are short
codes such as `"n"`, `"v5r"`, `"uk"` by default; `--expand-entities` gives
descriptions such as `"noun (common) (futsuumeishi)"`.

## Notes

- **Output guard** — `unzip` and `convert` exit with an error if the output
  exists. Pass `--force` when re-generating. `download` always replaces its own
  output and needs `--force` only to skip the up-to-date check.
- **Up-to-date check** — `download` compares the server's `Last-Modified` with
  the local file's modification time (or the `source-last-modified` metadata on
  a `gs://` object). EDRDG publishes a new file almost daily, around 03:30 UTC.
- **No partial files** — a failed command leaves any existing output untouched.
- **stdout is data only** — logs and download progress go to stderr, so
  `-o -` can be piped safely.
- **Size** — about 219,000 entries; the compact JSON is about 56 MB, `--pretty`
  about 135 MB. Conversion takes a couple of seconds.
- **Variants** — `--url http://ftp.edrdg.org/pub/Nihongo/JMdict.gz` fetches the
  multilingual file; glosses then carry other `lang` values. HTTPS is not
  served by EDRDG, and FTP is not supported.
- **Licence** — JMdict is the property of the Electronic Dictionary Research and
  Development Group and is licensed under CC BY-SA 4.0
  (https://www.edrdg.org/edrdg/licence.html). Anything that displays or
  redistributes the data or the JSON must acknowledge EDRDG and keep the same
  licence; mention this when the user plans to publish or ship the output.
