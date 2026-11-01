#!/bin/sh
# Daily Cloud Run job: refresh the JMdict download in the bucket, then
# convert it. The conversion always runs, as JMdict changes almost daily.
set -eu

: "${BUCKET:?BUCKET is required}"

jmdict-to-json download -o "gs://${BUCKET}/jmdict.gz"
jmdict-to-json convert -i "gs://${BUCKET}/jmdict.gz" -o "gs://${BUCKET}/jmdict.json" --force
