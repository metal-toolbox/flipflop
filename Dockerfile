FROM alpine:3.8 AS stage1

ARG IPMITOOL_REPO=https://github.com/ipmitool/ipmitool.git
ARG IPMITOOL_COMMIT=19d78782d795d0cf4ceefe655f616210c9143e62

WORKDIR /tmp
RUN apk add --update --upgrade --no-cache --virtual build-deps\
            alpine-sdk \
            automake \
            autoconf \
            libtool \
            openssl-dev \
            readline-dev \
    && git clone -b master ${IPMITOOL_REPO}

#
# cherry-pick'ed 1edb0e27e44196d1ebe449aba0b9be22d376bcb6
# to fix https://github.com/ipmitool/ipmitool/issues/377
#
WORKDIR /tmp/ipmitool
RUN git checkout ${IPMITOOL_COMMIT} \
    && git config --global user.email "github.ci@doesnot.existorg" \
    && git cherry-pick 1edb0e27e44196d1ebe449aba0b9be22d376bcb6 \
    && ./bootstrap \
    && ./configure \
        --prefix=/usr/local \
        --enable-ipmievd \
        --enable-ipmishell \
        --enable-intf-lan \
        --enable-intf-lanplus \
        --enable-intf-open \
    && make \
    && make install \
    && apk del build-deps

WORKDIR /tmp
RUN rm -rf /tmp/ipmitool

# Build a lean image with dependencies installed.
FROM alpine:3.8
COPY --from=stage1 / /

# required by ipmitool runtime
RUN apk add --update --upgrade --no-cache --virtual run-deps \
	    ca-certificates \
        libcrypto1.0 \
        musl \
        readline

COPY flipflop /usr/sbin/flipflop
RUN chmod +x /usr/sbin/flipflop

ENTRYPOINT ["/usr/sbin/flipflop"]
