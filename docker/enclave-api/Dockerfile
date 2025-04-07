FROM golang:1.24 AS build

RUN useradd -u 10001 dimo

WORKDIR /build
COPY . ./

RUN go mod tidy
RUN make build

FROM gcr.io/distroless/base AS final

LABEL maintainer="DIMO <hello@dimo.zone>"

COPY --from=build --chown=nonroot:nonroot/build/bin/sample-enclave-api /

EXPOSE 8080
EXPOSE 8888

ENTRYPOINT ["/sample-enclave-api"]
