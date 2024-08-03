# -*- coding: utf-8 -*-
# vim: ft=Dockerfile

### container - builder
FROM golang:1.19.10-bullseye AS build
LABEL maintainer="mindhunter86 <mindhunter86@vkom.cc>"

ARG GOAPP_MAIN_VERSION="devel"
ARG GOAPP_MAIN_BUILDTIME="N/A"

ENV MAIN_VERSION=$GOAPP_MAIN_VERSION
ENV MAIN_BUILDTIME=$GOAPP_MAIN_BUILDTIME

ENV DEBIAN_FRONTEND=noninteractive

# hadolint/hadolint - DL4006
SHELL ["/bin/bash", "-o", "pipefail", "-c"]

WORKDIR /usr/sources/alice
COPY . .

# skipcq: DOK-DL3018 i'm a badboy, disable this shit
RUN echo "ready" \
  && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X 'main.version=$MAIN_VERSION' -X 'main.buildtime=$MAIN_BUILDTIME'" -o alice cmd/alice/main.go cmd/alice/flags.go \
  && apt-get update && apt-get install --no-install-recommends -y upx-ucl \
  && upx -9 -k alice


### container - runner
###   for image debuging use tag :debug
FROM gcr.io/distroless/static-debian11:latest
LABEL maintainer="mindhunter86 <mindhunter86@vkom.cc>"

WORKDIR /usr/local/bin/
COPY --from=build --chmod=0555 /usr/sources/alice/alice alice

USER nobody
ENTRYPOINT ["/usr/local/bin/alice"]
CMD ["--help"]
