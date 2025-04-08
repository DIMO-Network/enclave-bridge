FROM stagex/bash:5.2.21@sha256:cb58f55d268fbe7ef629cda86e3a8af893066e4af7f26ef54748b6ad47bdaa66 AS bash
FROM stagex/make:4.4@sha256:df43f0cf3ac1ad91bf91baefb539e8df42c11b0954a6e2498322a5467deb81e3 AS make
FROM stagex/musl:sx2024.09.0@sha256:ad351b875f26294562d21740a3ee51c23609f15e6f9f0310e0994179c4231e1d AS musl
FROM stagex/zlib:1.3.1@sha256:96b4100550760026065dac57148d99e20a03d17e5ee20d6b32cbacd61125dbb6 AS zlib
FROM stagex/core-git:sx2025.03.0@sha256:5b0ce8741532026bb7e6f2302855a93057a27a7f38e596e9a7fb0e782f04d0f6 AS git

FROM stagex/pallet-go AS build

COPY --from=stagex/bash . /
COPY --from=make . /
COPY --from=musl . /
COPY --from=zlib . /
COPY --from=git . /

ENV CGO_ENABLED=0

WORKDIR /build
COPY . ./

RUN go mod tidy
RUN make build

FROM gcr.io/distroless/base AS final

LABEL maintainer="DIMO <hello@dimo.zone>"

USER nonroot:nonroot

ARG APP_NAME
COPY --from=build --chown=nonroot:nonroot /build/bin/${APP_NAME} /app

ENTRYPOINT ["/app"]