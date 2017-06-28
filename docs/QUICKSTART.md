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

- Cassandra 2.x / ScyllaDB 1.x: Primary application datastore. Usage is light, a shared C* installation should be fine in almost all cases.
- Vault: datastore for application secrets (AWS credentials, GitHub token, TLS cert/key). Token or AppID authentication is supported out of the box.
- Kafka: used as a message bus for build events, so that a build can be monitored from any node (not just the node physically running the build). The Kafka cluster does not need to be dedicated--Furan's usage is relatively light. Message TTL need only be as long as your longest expected Docker build. One hour should be more than sufficient.

Getting Started
---------------

``$ docker-compose up``

Dependencies and how to run Furan are best documented in ``docker-compose.yml``.

The Docker Compose setup will spin up and initialize all dependencies and then bring up Furan.

For builds and pushes to work with docker-compose, edit `local_dev_secrets.json` to include:

- GitHub token (for fetching repositories)
- dockercfg (for pushing to remote docker registries)
- AWS access key ID and secret key (for pushing failed build logs to an S3 bucket)

Production Examples
-------------------

[CoreOS cloud-init with RAM disk](https://github.com/dollarshaveclub/furan/blob/master/docs/coreos-ramdisk.yml)
