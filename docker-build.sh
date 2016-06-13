#!/bin/sh

# DO NOT RUN LOCALLY - For docker build only!
# WILL NUKE YOUR .git !

apk update
apk add git

if [[ -d ".git" ]]; then
  git log -1 --pretty="format:%h" > VERSION.txt
  git log -1 --pretty="format:%ai %s" > DESCRIPTION.txt
  rm -rf ./.git
else
  echo "unknown" > VERSION.txt
  echo "unknown" > DESCRIPTION.txt
fi

./build_protos.sh

go get -v
go build || exit 1
go install

rm -rf /go/src/*

apk del git
rm -rf /var/cache/apk/*
