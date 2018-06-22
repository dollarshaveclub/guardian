#!/bin/bash

echo "Starting..."
docker-compose -f e2e/docker-compose.yml up -d > /dev/null 2>&1

echo "Building..."
docker build . -f Dockerfile-e2e-circleci -t guardian-e2e:latest

echo "Running..."
docker run --network e2e_default guardian-e2e:latest /go/src/github.com/dollarshaveclub/guardian/e2e/circleci-e2e-runner-docker.sh
results=$?

echo "Stopping..."
docker-compose -f e2e/docker-compose.yml stop > /dev/null 2>&1

if [ "$results" != 0 ] ; then false; fi