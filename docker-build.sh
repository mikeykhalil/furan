#!/bin/sh

./build_protos.sh || exit 1

# install dumb-init for local use
apk update
apk add ca-certificates wget
update-ca-certificates
wget -O /bin/dumb-init https://github.com/Yelp/dumb-init/releases/download/v1.2.0/dumb-init_1.2.0_amd64
chmod +x /bin/dumb-init
cp /go/src/github.com/dollarshaveclub/furan/dc-run.sh /dc-run.sh
chmod a+x /dc-run.sh

go get -v || exit 1
go build || exit 1
go install || exit 1

rm -rf /go/src/*
rm -rf /go/pkg/*
rm -rf /var/cache/apk/*
