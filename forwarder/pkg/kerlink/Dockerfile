FROM debian:11

RUN apt-get update && \
  apt-get install -y curl binutils

RUN curl -Lo /usr/bin/opkg-build https://raw.githubusercontent.com/seife/opkg-utils/refs/heads/master/opkg-build && \
  chmod 755 /usr/bin/opkg-build

WORKDIR /build