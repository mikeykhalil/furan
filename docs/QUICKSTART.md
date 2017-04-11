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

Examples
--------

[CoreOS cloud-init with RAM disk](https://github.com/dollarshaveclub/furan/blob/master/docs/coreos-ramdisk.yml)
