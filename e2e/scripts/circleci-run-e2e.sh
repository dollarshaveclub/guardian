#!/bin/bash

echo "Building..."
docker build . -f Dockerfile-e2e-circleci -t guardian-e2e:latest


echo "Starting async guardian..."
docker-compose -f e2e/docker-compose-async.yml up -d --build --force-recreate > /dev/null 2>&1
sleep 30 # wait for guardian to finish starting up

echo "Running..."
docker run --network e2e_default guardian-e2e:latest /go/src/github.com/dollarshaveclub/guardian/e2e/scripts/circleci-e2e-runner-docker.sh
results=$?

echo "Stopping async guardian..."
docker-compose -f e2e/docker-compose-async.yml stop > /dev/null 2>&1

if [ "$results" != 0 ] ; then exit 1; fi

echo "Starting sync guardian..."
docker-compose -f e2e/docker-compose-sync.yml up -d --build --force-recreate > /dev/null 2>&1
sleep 30 # wait for guardian to finish starting up

echo "Running..."
docker run --network e2e_default --env SYNC=true guardian-e2e:latest /go/src/github.com/dollarshaveclub/guardian/e2e/scripts/circleci-e2e-runner-docker.sh
results=$?

echo "Stopping sync guardian..."
docker-compose -f e2e/docker-compose-sync.yml stop > /dev/null 2>&1

if [ "$results" != 0 ] ; then exit 1; fi