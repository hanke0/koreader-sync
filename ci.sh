#!/bin/bash

set -e

go run . "-db=:memory:" &
trap "exit" INT TERM
trap "kill 0" EXIT

sleep 2

host=127.0.0.1:9200

curl -d '{"username":"test","password":"test"}' \
    "$host/users/create"

curl --fail-with-body \
    -H "x-auth-user: test" \
    -H "x-auth-key: test" \
    "$host/users/auth"

curl -XPUT --fail-with-body \
    -H "x-auth-user: test" \
    -H "x-auth-key: test" \
    -d '{"progress": "0.5","percentage": 10,"document":"a.epub","device":"koreader","device_id":"111"}' \
    "$host/syncs/progress"

curl --fail-with-body \
    -H "x-auth-user: test" \
    -H "x-auth-key: test" \
    "$host/syncs/progress/a.epub"
