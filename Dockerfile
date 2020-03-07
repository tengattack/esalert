FROM golang:1.13-alpine3.10

ARG version
ARG proxy

# Download packages from aliyun mirrors
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories \
  && apk --update add --no-cache ca-certificates tzdata git

COPY . /go/src/github.com/tengattack/esalert/
WORKDIR /go/src/github.com/tengattack/esalert/
RUN http_proxy=$proxy https_proxy=$proxy git clone https://github.com/teejaded/gluasocket /go/src/github.com/nubix-io/gluasocket \
  && http_proxy=$proxy https_proxy=$proxy go get -d -v ./...
WORKDIR /go/src/github.com/tengattack/esalert/cmd/esalert/
# FIXME: cgo
RUN GOOS=linux CGO_ENABLED=0 go build -ldflags "-X main.Version=$version"

FROM scratch

#COPY --from=0 /usr/share/ca-certificates /usr/share/ca-certificates
COPY --from=0 /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=0 /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=0 /etc/passwd /etc/passwd

COPY --from=0 /go/src/github.com/tengattack/esalert/cmd/esalert/esalert /

WORKDIR /etc/esalert/

USER nobody

ENTRYPOINT ["/esalert"]
