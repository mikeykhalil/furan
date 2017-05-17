#!/bin/sh

protoc -I ./protos ./protos/models.proto --go_out=plugins=grpc:lib
protoc -I ./protos ./protos/models.proto --go_out=plugins=grpc:rpcclient
if [[ $(uname) == "Darwin" ]]; then
  SED="gsed"
else
  SED="sed"
fi
$SED -i 's/package cmd/package rpcclient/g' ./rpcclient/models.pb.go
