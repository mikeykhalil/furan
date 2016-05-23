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
    "tags": ["master"],
    "tag_with_commit_sha": true,
    "ref": {
        "branch": "master",
        // OR
        "sha": "xxxxxxxxxxxxx"
    },
  },
  "push": {
    "registry": {
      "repo": "quay.io/dollarshaveclub/foobar"
    },
    // OR
    "s3": {
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
      "ref": {
          "branch": "master",
      },
    },
    "push": {
      "registry": {
        "image_repo": "quay.io/dollarshaveclub/foobar",
        "tags": ["master"],
        "tag_with_commit_sha": true,
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
  - pullingSquashed
  - success
  - buildFailure
  - pushFailure
  - pullSquashedFailure
