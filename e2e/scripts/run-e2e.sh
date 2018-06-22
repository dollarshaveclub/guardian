#!/bin/bash

echo "Starting..."
docker-compose -f e2e/docker-compose.yml up -d > /dev/null 2>&1

echo "Running..."
go test ./e2e/
results=$?

echo "Stopping..."
docker-compose -f e2e/docker-compose.yml stop > /dev/null 2>&1

if [ "$results" != 0 ] ; then false; fi