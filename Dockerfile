# -*- coding: utf-8 -*-
# vim: ft=Dockerfile

# container - builder
FROM golang:1.19.10-alpine AS build
LABEL maintainer="mindhunter86 <mindhunter86@vkom.cc>"

ARG GOAPP_MAIN_VERSION="devel"
ARG GOAPP_MAIN_BUILDTIME="N/A"

ENV MAIN_VERSION=$GOAPP_MAIN_VERSION
ENV MAIN_BUILDTIME=$GOAPP_MAIN_BUILDTIME

# hadolint/hadolint - DL4006
SHELL ["/bin/ash", "-eo", "pipefail", "-c"]

WORKDIR /usr/sources/alice
COPY . .

ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64

# skipcq: DOK-DL3018 i'm a badboy, disable this shit
RUN echo "ready" \
  && go build -trimpath -ldflags="-s -w -X 'main.version=$MAIN_VERSION' -X 'main.buildtime=$MAIN_BUILDTIME'" -o alice cmd/alice/main.go cmd/alice/flags.go \
  && apk add --no-cache upx \
  && upx -9 -k alice \
  && echo "nobody:x:65534:65534:nobody:/usr/local/bin:/bin/false" > etc_passwd


# container - runner
FROM scratch
LABEL maintainer="mindhunter86 <mindhunter86@vkom.cc>"

WORKDIR /usr/local/bin/
COPY --from=build /usr/sources/alice/etc_passwd /etc/passwd
COPY --from=build --chmod=0555 /usr/sources/alice/alice alice

USER nobody
ENTRYPOINT ["/usr/local/bin/alice"]
CMD ["--help"]
