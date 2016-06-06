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

go get -v
go build
go install

apk del git
rm -rf /var/cache/apk/*
