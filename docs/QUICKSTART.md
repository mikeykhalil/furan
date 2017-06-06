Quickstart
==========

Architecture
------------

Furan is deployed as a set of independent build nodes that can be accessed in a
round-robin fashion. The HTTPS API could be accessed behind a load balancer, while
the GRPC API could be used through a service discovery mechanism or a hard-coded
list of nodes.

The two main operations are triggering builds and monitoring builds. A trigger is
asynchronous and returns immediately with a build ID. The user can then optionally
monitor the build which will stream build and push events in realtime. The user
also has the option of polling the build status instead of realtime monitoring
until it finishes.

Dependencies
------------

- Cassandra 2.x / ScyllaDB 1.x: Primary application datastore.
- Vault: datastore for application secrets (AWS credentials, GitHub token, TLS cert/key)
- Kafka: used as a message bus for build events, so that a build can be monitored from any node (not just the node physically running the build)

Getting Started
---------------

``$ docker-compose up``

Dependencies and how to run Furan are best documented in ``docker-compose.yml``.

Production Examples
-------------------

[CoreOS cloud-init with RAM disk](https://github.com/dollarshaveclub/furan/blob/master/docs/coreos-ramdisk.yml)
