#!/bin/bash

set -e

host=127.0.0.1:9200
go run . "-db=:memory:" -addr=$host &
trap "exit" INT TERM
trap "kill 0" EXIT

sleep 2


curl -d '{"username":"test","password":"test"}' \
    "$host/users/create"

curl --fail-with-body \
    -H "x-auth-user: test" \
    -H "x-auth-key: test" \
    "$host/users/auth"

curl -XPUT --fail-with-body \
    -H "x-auth-user: test" \
    -H "x-auth-key: test" \
    -d '{"percentage":0.3532,"progress":"\/body\/DocFragment[15]\/body\/p[176]\/text().52","document":"b98261a836749dee4a24fbe4cddbf62e","device_id":"E234BF54A4DE4B3F85D785A1CB355F65","device":"hitv205n"}' \
    "$host/syncs/progress"

curl --fail-with-body \
    -H "x-auth-user: test" \
    -H "x-auth-key: test" \
    "$host/syncs/progress/b98261a836749dee4a24fbe4cddbf62e"
