![Furan](https://s3.amazonaws.com/dsc-misc/furan.jpg)

Your anti-qwait solution

API
===

POST ``/build``
---------------

Body:

```javascript
{
  "build": {
    "github_repo": "dollarshaveclub/foobar",
    "tags": ["master"],   // tags only (not including repo/name)
    "tag_with_commit_sha": true,
    "ref": "master",   // commit SHA or branch or tag
  },
  "push": {
    "registry": {
      "repo": "quay.io/dollarshaveclub/foobar"
    },
    // OR
    "s3": {
      "region": "us-west-2",
      "bucket": "foobar",
      "key_prefix": "myfolder/"
    }
  }
}
```

Response:

```json
{
  "build_id": "56aeda1c-6736-4657-8440-77972b103fee"
}
```

GET ``/build/{id}``
-------------------

Response:

```javascript
{
  "build_id": "56aeda1c-6736-4657-8440-77972b103fee",
  "request": {
    "build": {
      "github_repo": "dollarshaveclub/foobar",
      "tags": ["master"],
      "tag_with_commit_sha": true,
      "ref": "master",
    },
    "push": {
      "registry": {
        "repo": "quay.io/dollarshaveclub/foobar"
      }
    },
  "state": "building",
  "failed": false,
  "started": "2016-05-19T07:33:54.691073",
  "completed": "",
  "duration": ""
}
```

Possible states:
  - building
  - pushing
  - success
  - buildFailure
  - pushFailure
