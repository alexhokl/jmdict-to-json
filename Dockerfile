FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /jmdict-to-json .

# Alpine rather than scratch, as job.sh needs a shell to run the commands one
# after another in a Cloud Run job.
FROM alpine:3.24
RUN apk add --no-cache ca-certificates
COPY --from=build /jmdict-to-json /usr/local/bin/jmdict-to-json
COPY job.sh /job.sh

USER 65532:65532

# The image works as the plain CLI; the Cloud Run job runs /job.sh instead.
ENTRYPOINT ["/usr/local/bin/jmdict-to-json"]
