#!/bin/sh

./build_protos.sh || exit 1

go get -v || exit 1
go build || exit 1
go install || exit 1

rm -rf /go/src/*
rm -rf /go/pkg/*
