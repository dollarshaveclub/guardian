#!/bin/sh

cat /config/envoy.yaml.tmpl | envtpl > /config/envoy.yaml
envoy -c /config/envoy.yaml --service-cluster guardian-cluster --service-node guarian-node -l $ENVOY_LOG_LEVEL
