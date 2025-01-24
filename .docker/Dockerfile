FROM golang:1.23.5-alpine AS build

RUN addgroup -g 501 app \
    && adduser --disabled-password \
        --gecos "" \
        --uid 501 \
        --ingroup app \
        --no-create-home \
        app \
    && grep app /etc/passwd > /etc/passwd_app

WORKDIR /build

RUN apk update && apk add --no-cache \
    build-base \
    gcc \
    musl-dev \
    git

RUN go env -w GOMODCACHE=/root/.cache/go-build
RUN go env -w CGO_ENABLED=1

COPY . .

RUN go mod download && \
    go build -v -o bin/app ./cmd/main.go && \
    go build -v -o bin/cli ./cmd/cli/main.go

FROM alpine AS build-info

ARG GIT_REV
ARG GIT_VERSION
ARG GIT_URL
ARG BUILD_DATE

WORKDIR /build

RUN echo "GIT_COMMIT_HASH=$GIT_REV"  > buildinfo && \
    echo "GIT_VERSION=$GIT_VERSION" >> buildinfo && \
    echo "GIT_URL=$GIT_URL"         >> buildinfo && \
    echo "BUILD_DATE=$BUILD_DATE"   >> buildinfo

FROM alpine

WORKDIR /usr/src/

COPY --from=build /etc/passwd_app /etc/passwd
COPY --from=build /build/bin/app /usr/src/app
COPY --from=build /build/bin/cli /usr/src/cli
COPY --from=build-info /build/buildinfo /usr/src/buildinfo

RUN mkdir /usr/src/data
RUN chown -R 501:501 /usr/src/
USER app

CMD ["/usr/src/app"]
