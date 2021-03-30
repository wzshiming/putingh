FROM golang:alpine AS builder
WORKDIR /go/src/github.com/wzshiming/putingh/
COPY . .
ENV CGO_ENABLED=0
RUN go install ./cmd/putingh

FROM alpine
COPY --from=builder /go/bin/putingh /usr/local/bin/
ENTRYPOINT [ "/usr/local/bin/putingh" ]