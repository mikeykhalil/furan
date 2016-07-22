#!/bin/sh

# DO NOT RUN LOCALLY - For docker build only!
# WILL NUKE YOUR .git !

apk update || exit 1
apk add git || exit 1

if [[ -d ".git" ]]; then
  git log -1 --pretty="format:%h" > VERSION.txt
  git log -1 --pretty="format:%ai %s" > DESCRIPTION.txt
  rm -rf ./.git
else
  echo "unknown" > VERSION.txt
  echo "unknown" > DESCRIPTION.txt
fi

./build_protos.sh || exit 1

go get -v || exit 1
go build || exit 1
go install || exit 1

rm -rf /go/src/*

apk del git
rm -rf /var/cache/apk/*
