FROM gitpod/workspace-base:latest

ENV GO_VERSION=1.18.5
RUN curl -fsSL https://dl.google.com/go/go${GO_VERSION}.linux-amd64.tar.gz | tar xzs \
    && echo "export GOPATH=$HOME/go\nexport PATH=$HOME/go/bin:$PATH" >> $HOME/.bashrc
