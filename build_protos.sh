#!/bin/sh

protoc -I ./protos ./protos/models.proto --go_out=plugins=grpc:cmd
protoc -I ./protos ./protos/models.proto --go_out=plugins=grpc:rpcclient
gsed -i 's/package cmd/package rpcclient/g' ./rpcclient/models.pb.go
