FROM ubuntu:22.04 as base 

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

WORKDIR /root/go/src/github.com/ccfish2
RUN  git clone https://github.com/ccfish2/controllerPoweredByDI.git && git clone https://github.com/ccfish2/infra.git && \
     export PATH=$PATH:/usr/local/go/bin

WORKDIR /root/go/src/github.com/ccfish2/controllerPoweredByDI
CMD [ "go run main.go" ]