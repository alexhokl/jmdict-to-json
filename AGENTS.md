# AGENTS.md

Cobra CLI (`github.com/alexhokl/jmdict-to-json`) that downloads the JMdict
dictionary from EDRDG, unzips it and converts its XML to JSON. Also runs daily
as a Cloud Run job (defined in the `cluster` repository) via `job.sh`.

## Commands

- `task build`, `task test`, `task lint`, `task sec`, `task coverage`,
  `task docker-build`
- Single test: `go test ./... -run TestName -v`

Run `task lint` and `task test` before considering a change done.

## Architecture

- `cmd/` holds the three subcommands, `download`, `unzip` and `convert`. There
  is deliberately no all-in-one command; `job.sh` chains them on Cloud Run.
  `run()` in `root.go` takes the args and streams so tests drive the real
  command line.
- `internal/jmdict` streams `<entry>` elements one at a time (constant memory)
  into camelCase JSON, as an array, an indented array or JSON Lines. The `xml*`
  structs mirror the DTD; the exported structs are the JSON contract.
- Entities (`&n;`, `&v5r;`, ...) come from the DOCTYPE of the input, never a
  hard-coded list. By default each maps to its own name, keeping codes;
  `--expand-entities` maps them to their descriptions.
- `internal/storage` routes each path to stdin/stdout (`-`), the local
  filesystem, or GCS (`gs://bucket/object`, created lazily so local use needs
  no credentials). Writes are invisible until `Commit`: a temp file and rename
  locally, an atomic upload with a `DoesNotExist` precondition on GCS.
- `internal/fetch` does the conditional GET. The server's `Last-Modified` is
  stored as the file mtime locally and as the `source-last-modified` object
  metadata on GCS, and sent back as `If-Modified-Since`.
- `internal/logging` renames slog keys to Cloud Logging's `severity`/`message`/
  `timestamp` for `--log-format json`.

## Conventions

- `unzip` and `convert` fail when the output exists unless `--force`;
  `download` always replaces its output, as keeping it current is its job.
- Every error is logged once at ERROR by `run()`. The alert policy in
  `cluster` filters on `severity>=ERROR` for the job, so keep that contract.
- The default URL is HTTP: Go has no FTP client and EDRDG does not serve HTTPS.
- Tests never call EDRDG or GCS; use `httptest`, the fixture in
  `internal/jmdict/testdata/sample.xml`, and `fakeGCS` in
  `internal/storage/gcs_test.go` (a fake Cloud Storage API reached through
  `STORAGE_EMULATOR_HOST`).
- The runtime image is Alpine (not scratch) because `job.sh` needs a shell.
  The image's entrypoint is the CLI; the Cloud Run job overrides it with
  `/job.sh`.
- JMdict data, and the JSON made from it, is CC BY-SA 4.0 from EDRDG (the code
  is MIT). Keep the attribution in the README, the root command's help text and
  the bucket's `LICENSE.txt` in `cluster`, and keep the job's update schedule at
  least monthly, as the licence requires regular updates.
- Keep the agent skill `skills/jmdict-to-json/SKILL.md` in sync when commands,
  flags, defaults or the JSON format change.
- Releases: `task tag` then push the tag; Cloud Build tests, builds and pushes
  the image. The image tag of the job is bumped by hand in `cluster`.
