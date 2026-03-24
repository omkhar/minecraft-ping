FROM golang:1.26.1-bookworm

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    ca-certificates \
    docker.io \
    git \
    unzip \
  && rm -rf /var/lib/apt/lists/*

RUN groupadd -r builder \
  && useradd -r -g builder -d /home/builder -m builder

WORKDIR /workspace

USER builder
