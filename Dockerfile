FROM golang:1.6-alpine

RUN mkdir -p /go/src/github.com/dollarshaveclub/furan
ADD . /go/src/github.com/dollarshaveclub/furan

WORKDIR /go/src/github.com/dollarshaveclub/furan
RUN ./docker-build.sh

CMD ["/go/bin/furan"]
