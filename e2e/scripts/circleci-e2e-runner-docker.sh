#!/bin/bash

cd /go/src/github.com/dollarshaveclub/guardian

COMMIT="e2e" make cli
# TODO: Refactor tests to be less brittle and not require sequential runs
go test ./e2e -p 1 -redis-addr="redis:6379" -envoy-addr="envoy:8080"
