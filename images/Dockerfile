FROM ubuntu:plucky-20241213 AS base

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y git && \
    apt-get install -y make && \
    apt-get install -y wget && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

WORKDIR  /workspace
RUN wget https://go.dev/dl/go1.23.4.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go1.23.4.linux-amd64.tar.gz && rm -f go1.23.4.linux-amd64.tar.gz && \
    export PATH=$PATH:/usr/local/go/bin

ARG GIT_USER
ENV GIT_USER=${GIT_USER}
ARG GIT_TOKEN
ENV GIT_TOKEN=${GIT_TOKEN}
WORKDIR /root/go/src/github.com/ccfish2
RUN  git clone https://${GIT_USER}:${GIT_TOKEN}@github.com/ccfish2/controllerPoweredByDI.git && \
     git clone https://${GIT_USER}:${GIT_TOKEN}@github.com/ccfish2/infra.git && \
     git clone https://${GIT_USER}:${GIT_TOKEN}@github.com/ccfish2/dolphin.git && \
     export PATH=$PATH:/usr/local/go/bin

FROM base AS release
WORKDIR /root/go/src/github.com/ccfish2/controllerPoweredByDI