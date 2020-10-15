#!/bin/bash

echo "Starting async guardian..."
docker-compose -f e2e/docker-compose-async.yml up -d --build --force-recreate > /dev/null 2>&1
[[ $? -gt 0 ]] && exit 1
secs=30 # wait for guardian to finish starting up
while [ $secs -ge 0 ]; do
   echo -ne "Waiting for guardian to start up...$secs\033[0K\r"
   sleep 1
   : $((secs--))
done

printf "\nRunning tests...\n"
go test -p 1 ./e2e/
results=$?

echo "Stopping async guardian..."
docker-compose -f e2e/docker-compose-async.yml stop > /dev/null 2>&1

if [ "$results" != 0 ] ; then exit 1; fi

echo "Starting sync guardian..."
docker-compose -f e2e/docker-compose-sync.yml up -d --build --force-recreate > /dev/null 2>&1
[[ $? -gt 0 ]] && exit 1
secs=30 # wait for guardian to finish starting up
while [ $secs -ge 0 ]; do
   echo -ne "Waiting for guardian to start up...$secs\033[0K\r"
   sleep 1
   : $((secs--))
done

printf "\nRunning tests...\n"
SYNC=true go test -p 1 ./e2e/
results=$?

echo "Stopping sync guardian..."
docker-compose -f e2e/docker-compose-sync.yml stop > /dev/null 2>&1

if [ "$results" != 0 ] ; then exit 1; fi
