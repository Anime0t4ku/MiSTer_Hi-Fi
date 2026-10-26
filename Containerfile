FROM docker.io/library/ubuntu:22.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y curl xz-utils zip file cmake git ca-certificates build-essential && rm -rf /var/lib/apt/lists/*
RUN curl -fL https://go.dev/dl/go1.23.4.linux-amd64.tar.gz | tar -xz -C /usr/local
RUN curl -fL "https://developer.arm.com/-/media/Files/downloads/gnu-a/10.2-2020.11/binrel/gcc-arm-10.2-2020.11-x86_64-arm-none-linux-gnueabihf.tar.xz" | tar -xJ -C /opt
RUN curl -fsSL https://sh.rustup.rs | sh -s -- -y --profile minimal --target armv7-unknown-linux-gnueabihf
ENV PATH=/usr/local/go/bin:/root/.cargo/bin:/opt/gcc-arm-10.2-2020.11-x86_64-arm-none-linux-gnueabihf/bin:$PATH
ENV CC=arm-none-linux-gnueabihf-gcc
WORKDIR /src
