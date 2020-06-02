#!/bin/bash

cd /go/src/github.com/dollarshaveclub/guardian

COMMIT="e2e" make cli
go test ./e2e -p 1 -redis-addr="redis:6379" -envoy-addr="envoy:8080"
