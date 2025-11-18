FROM golang:1.24-bookworm AS build

ENV CGO_ENABLED=1

ARG BUILDPLATFORM
ARG TARGETPLATFORM
ARG BUILD=dev

WORKDIR /build
RUN apt-get update && apt-get install -y --no-install-recommends \
    libgdbm-dev \
    git \
    && rm -rf /var/lib/apt/lists/*
RUN git clone https://github.com/grassrootseconomics/go-vise go-vise
COPY . ./sarafu-vise

WORKDIR /build/sarafu-vise/services/registration
RUN echo "Compiling go-vise files"
RUN make VISE_PATH=/build/go-vise -B

WORKDIR /build/sarafu-vise
RUN echo "Building on $BUILDPLATFORM, building for $TARGETPLATFORM"
RUN go mod download
RUN go build -tags logtrace,online -o sarafu-at -ldflags="-X main.build=${BUILD} -s -w" cmd/africastalking/main.go

FROM debian:bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    libgdbm-dev \
    ca-certificates \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /service

COPY --from=build /build/sarafu-vise/sarafu-at .
COPY --from=build /build/sarafu-vise/LICENSE .
COPY --from=build /build/sarafu-vise/README.md .
COPY --from=build /build/sarafu-vise/services ./services
COPY --from=build /build/sarafu-vise/.env.example .
RUN mv .env.example .env

EXPOSE 7123

CMD ["./sarafu-at"]
