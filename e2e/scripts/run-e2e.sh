#!/bin/bash

echo "Starting async guardian..."
docker-compose -f e2e/docker-compose-async.yml up -d --build --force-recreate > /dev/null 2>&1
sleep 30 # wait for guardian to finish starting up

echo "Running..."
GOCACHE=off go test ./e2e/
results=$?

echo "Stopping async guardian..."
docker-compose -f e2e/docker-compose-async.yml stop > /dev/null 2>&1

if [ "$results" != 0 ] ; then exit 1; fi

echo "Starting sync guardian..."
docker-compose -f e2e/docker-compose-sync.yml up -d --build --force-recreate > /dev/null 2>&1
sleep 30 # wait for guardian to finish starting up

echo "Running..."
GOCACHE=off SYNC=true go test ./e2e/
results=$?

echo "Stopping sync guardian..."
docker-compose -f e2e/docker-compose-sync.yml stop > /dev/null 2>&1

if [ "$results" != 0 ] ; then exit 1; fi