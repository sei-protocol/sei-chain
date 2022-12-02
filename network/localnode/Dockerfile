FROM alpine:3.7

RUN apk update && \
    apk upgrade && \
    apk --no-cache add go

VOLUME [ /sei-chain ]
WORKDIR /sei-chain
EXPOSE 26656 26657
CMD ["/usr/bin/init.sh"]

STOPSIGNAL SIGTERM
COPY init.sh /usr/bin/init.sh

