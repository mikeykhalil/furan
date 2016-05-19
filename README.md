![Furan](https://s3.amazonaws.com/dsc-misc/furan.jpg)

Your anti-qwait solution

API
===

POST ``/build``
---------------

Body:

```javascript
{
  "source_repo": "https://github.com/dollarshaveclub/foobar",
  "source_branch": "master",
  "image_repo": "quay.io/dollarshaveclub/foobar",
  "tags": ["master"],
  "tag_with_commit_sha": true,
  "pull_squashed_image": true  // https://docs.quay.io/guides/squashed-images.html
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
    "source_repo": "https://github.com/dollarshaveclub/foobar",
    "source_branch": "master",
    "image_repo": "quay.io/dollarshaveclub/foobar",
    "tags": ["master"],
    "tag_with_commit_sha": true,
    "pull_squashed_image": true
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
