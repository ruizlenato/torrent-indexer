##################### Build executable binary
FROM golang:alpine AS builder

WORKDIR /go/src/app
COPY . .

RUN apk update && apk add --no-cache git
RUN go get -d -v
RUN go build -o /go/bin/torrent-index


##################### Build Alpine Image
FROM alpine:latest

COPY --from=builder /go/bin/torrent-index /go/bin/torrent-index

RUN apk --no-cache add ca-certificates

EXPOSE 7006
ENTRYPOINT ["/go/bin/torrent-index"]