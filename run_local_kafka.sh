#!/bin/bash

start() {
  docker run -d --name zookeeper -p 2181:2181 confluent/zookeeper
  sleep 5 #let zookeeper get settled
  docker run -d --name kafka -p 9092:9092 --link zookeeper:zookeeper --env KAFKA_ADVERTISED_HOST_NAME=$(docker-machine ip default) confluent/kafka
  docker run -d --name schema-registry -p 8081:8081 --link zookeeper:zookeeper --link kafka:kafka confluent/schema-registry
  docker run -d --name rest-proxy -p 8082:8082 --link zookeeper:zookeeper --link kafka:kafka --link schema-registry:schema-registry confluent/rest-proxy
  docker run -d --name kafka-manager -p 9000:9000 -e ZK_HOSTS="zookeeper:2181" --link zookeeper:zookeeper -e APPLICATION_SECRET=letmein sheepkiller/kafka-manager
}

stop() {
  docker stop kafka schema-registry rest-proxy kafka-manager zookeeper
  docker rm kafka schema-registry rest-proxy kafka-manager zookeeper
}

${1}
