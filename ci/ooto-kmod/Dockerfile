FROM alpine

RUN ["apk", "add", "kmod"]

ARG KERNEL_VERSION

RUN mkdir -p /lib/modules/${KERNEL_VERSION}

COPY ooto_ci.ko /lib/modules/${KERNEL_VERSION}/

RUN ["depmod"]
