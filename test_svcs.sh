#!/bin/sh


#docker run -d --name scylla -p 9042:9042 scylladb/scylla:1.2.0 /bin/bash -c '/start-scylla && tail -f /dev/null'
docker run -d --name scylla -p 9042:9042 scylladb/scylla:1.3.1 --broadcast-address $(docker-machine ip default) --broadcast-rpc-address $(docker-machine ip default) --developer-mode 1
