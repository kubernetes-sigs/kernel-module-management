FROM alpine

RUN ["apk", "add", "kmod"]

ARG KERNEL_VERSION

RUN mkdir -p /opt/lib/modules/${KERNEL_VERSION}/extra

COPY ooto_ci_a.ko ooto_ci_b.ko /opt/lib/modules/${KERNEL_VERSION}/extra/

RUN ["depmod", "-b", "/opt"]
