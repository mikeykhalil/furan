#!/bin/sh

protoc -I ./protos ./protos/models.proto --go_out=plugins=grpc:cmd
