#!/bin/dumb-init /bin/sh

echo "starting (delay)"
sleep 20
exec /go/bin/furan -k -i -n scylla -f kafka:9092 server --log-to-sumo=false --consul-addr consul:8500
