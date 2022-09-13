FROM golang:1.18.5-alpine as builder
RUN apk add --no-cache git gcc make libc-dev linux-headers
RUN set -eux; apk add --no-cache ca-certificates build-base

ADD https://github.com/CosmWasm/wasmvm/releases/download/v1.0.0/libwasmvm_muslc.aarch64.a /lib/libwasmvm_muslc.aarch64.a
ADD https://github.com/CosmWasm/wasmvm/releases/download/v1.0.0/libwasmvm_muslc.x86_64.a /lib/libwasmvm_muslc.x86_64.a
RUN sha256sum /lib/libwasmvm_muslc.aarch64.a | grep 7d2239e9f25e96d0d4daba982ce92367aacf0cbd95d2facb8442268f2b1cc1fc
RUN sha256sum /lib/libwasmvm_muslc.x86_64.a | grep f6282df732a13dec836cda1f399dd874b1e3163504dbd9607c6af915b2740479

RUN apk --print-arch > ./architecture
RUN cp /lib/libwasmvm_muslc.$(cat ./architecture).a /lib/libwasmvm_muslc.a
RUN rm ./architecture

WORKDIR /src
COPY go.mod .
COPY go.sum .
ENV GO111MODULE=on
RUN go mod download
COPY . .
RUN LEDGER_ENABLED=false BUILD_TAGS=muslc LINK_STATICALLY=true make install
RUN go install github.com/cosmos/gex@latest

FROM alpine:latest
RUN apk add --no-cache git gcc make libc-dev linux-headers curl jq 
COPY --from=builder /go/bin/* /usr/local/bin/

VOLUME /apps/data
WORKDIR /apps/data
EXPOSE 26656 26657 9090
