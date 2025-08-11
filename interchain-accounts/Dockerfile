# Compile
FROM golang:alpine AS builder
WORKDIR /src/app/
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN for bin in cmd/*; do CGO_ENABLED=0 go build -o=/usr/local/bin/$(basename $bin) ./cmd/$(basename $bin); done


# Add to a distroless container
FROM gcr.io/distroless/base
COPY --from=builder /usr/local/bin /usr/local/bin
USER nonroot:nonroot
CMD ["icad start"]
