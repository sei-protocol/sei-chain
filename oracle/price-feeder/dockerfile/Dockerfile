FROM golang:alpine AS oracle
RUN apk add --no-cache git make gcc libc-dev
WORKDIR /go/src/github.com/sei-protocol
RUN git clone https://github.com/sei-protocol/sei-chain.git
WORKDIR /go/src/github.com/sei-protocol/sei-chain/oracle/price-feeder
RUN wget -O /lib/libwasmvm_muslc.a https://github.com/CosmWasm/wasmvm/releases/download/v1.0.0/libwasmvm_muslc.x86_64.a
ARG VERSION=main
RUN git checkout ${VERSION}
RUN COMMIT="$(git rev-parse HEAD)"; BUILD_TAGS=muslc CGO_ENABLED=1 go install -mod=readonly -tags "muslc" -ldflags "-X price-feeder/cmd.Version=${VERSION}-$COMMIT -X price-feeder/cmd.Commit=$COMMIT" ./

FROM golang:alpine AS sei
RUN apk add --no-cache git make gcc libc-dev
WORKDIR /go/src/github.com/sei-protocol
RUN git clone https://github.com/sei-protocol/sei-chain.git
WORKDIR /go/src/github.com/sei-protocol/sei-chain
RUN wget -O /lib/libwasmvm_muslc.a https://github.com/CosmWasm/wasmvm/releases/download/v1.0.0/libwasmvm_muslc.x86_64.a
ARG VERSION=main
RUN git checkout ${VERSION}
RUN COMMIT="$(git rev-parse HEAD)"; BUILD_TAGS=muslc CGO_ENABLED=1 go install -mod=readonly -tags "muslc" -ldflags "-X price-feeder/cmd.Version=${VERSION}-$COMMIT -X price-feeder/cmd.Commit=$COMMIT" ./cmd/seid

FROM alpine:latest
COPY --from=oracle /go/bin/price-feeder /usr/local/bin
COPY --from=sei /go/bin/seid /usr/local/bin
RUN apk add --no-cache bash
RUN apk add --no-cache gcc
COPY commands.sh /scripts/commands.sh
RUN ["chmod", "+x", "/scripts/commands.sh"]
ENTRYPOINT ["/scripts/commands.sh"]