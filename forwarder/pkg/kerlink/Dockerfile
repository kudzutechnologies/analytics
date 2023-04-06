FROM debian:11

RUN apt-get update && \
  apt-get install -y curl binutils

RUN curl -Lo /usr/bin/opkg-build https://git.yoctoproject.org/cgit/cgit.cgi/opkg-utils/plain/opkg-build && \
  chmod 755 /usr/bin/opkg-build

WORKDIR /build