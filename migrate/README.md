Migrations
==========

```bash
$ go get -v -u github.com/mattes/migrate
# create a migration:
$ migrate -url cassandra://10.10.29.184/thalamus -path ./migrations create my_new_migration
# run migrations:
$ migrate -url cassandra://10.10.29.184/thalamus -path ./migrations up
# or
$ migrate -url cassandra://10.10.29.184/thalamus -path ./migrations +1
# or
migrate -url cassandra://10.10.29.184/thalamus -path ./migrations goto 5
# etc
```
