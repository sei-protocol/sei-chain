FROM ubuntu:latest
RUN apt-get update && \
    apt-get install -y make

VOLUME [ /sei-protocol ]
WORKDIR /sei-protocol/sei-chain

EXPOSE 26656 26657

CMD ["/usr/bin/init.sh"]

STOPSIGNAL SIGTERM

COPY init.sh /usr/bin/init.sh