#!/bin/sh

protoc -I ./protos ./protos/models.proto --go_out=plugins=grpc:generated/pb
protoc -I ./protos ./protos/models.proto --go_out=plugins=grpc:rpcclient
if [[ $(uname) == "Darwin" ]]; then
  SED="gsed"
else
  SED="sed"
fi
$SED -i 's/package pb/package rpcclient/g' ./rpcclient/models.pb.go
