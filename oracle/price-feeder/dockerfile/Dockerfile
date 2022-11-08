FROM golang:alpine AS oracle
RUN apk add --no-cache git make gcc libc-dev
WORKDIR /go/src/github.com/team-kujira
RUN git clone https://github.com/Team-Kujira/oracle-price-feeder.git
WORKDIR /go/src/github.com/team-kujira/oracle-price-feeder
RUN wget -O /lib/libwasmvm_muslc.a https://github.com/CosmWasm/wasmvm/releases/download/v1.0.0/libwasmvm_muslc.x86_64.a
ARG VERSION=master
RUN git checkout ${VERSION}
RUN COMMIT="$(git rev-parse HEAD)"; BUILD_TAGS=muslc CGO_ENABLED=1 go install -mod=readonly -tags "muslc" -ldflags "-X price-feeder/cmd.Version=${VERSION}-$COMMIT -X price-feeder/cmd.Commit=$COMMIT" ./...

FROM golang:alpine AS core
RUN apk add --no-cache git make gcc libc-dev
WORKDIR /go/src/github.com/team-kujira
RUN git clone https://github.com/Team-Kujira/core.git
WORKDIR /go/src/github.com/team-kujira/core
RUN wget -O /lib/libwasmvm_muslc.a https://github.com/CosmWasm/wasmvm/releases/download/v1.0.0/libwasmvm_muslc.x86_64.a
ARG VERSION=master
RUN git checkout ${VERSION}
RUN COMMIT="$(git rev-parse HEAD)"; BUILD_TAGS=muslc CGO_ENABLED=1 go install -mod=readonly -tags "muslc" -ldflags "-X price-feeder/cmd.Version=${VERSION}-$COMMIT -X price-feeder/cmd.Commit=$COMMIT" ./...

FROM alpine:latest 
COPY --from=oracle /go/bin/price-feeder /usr/local/bin
COPY --from=core /go/bin/kujirad /usr/local/bin
RUN apk add --no-cache bash
RUN apk add --no-cache gcc
COPY commands.sh /scripts/commands.sh
RUN ["chmod", "+x", "/scripts/commands.sh"]
ENTRYPOINT ["/scripts/commands.sh"]