FROM golang AS builder

ENV GOPATH /go
ENV PATH ${GOPATH}/bin:$PATH
ENV GO111MODULE=on
ENV CGO_ENABLED 0

WORKDIR /app
COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN make build

FROM alpine

LABEL maintainer="Eugene Fureev <fureev@gmail.com>"
LABEL author="Eugene Fureev <fureev@gmail.com>"

COPY --from=builder "/app/bin/go-swagger-merger" /app/go-swagger-merger

WORKDIR /app
