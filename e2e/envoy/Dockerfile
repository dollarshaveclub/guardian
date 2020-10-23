FROM docker.io/envoyproxy/envoy-alpine:v1.15.2

ENV ENVOY_LOG_LEVEL=trace
ENV ENVOY_LISTEN_ADDRESS=0.0.0.0
ENV ENVOY_LISTEN_PORT=8080
ENV ENVOY_UPSTREAM_ADDRESS=upstream
ENV ENVOY_UPSTREAM_PORT=5678
ENV ENVOY_RATELIMIT_ADDRESS=guardian
ENV ENVOY_RATELIMIT_PORT=3000
ENV ENVOY_RATELIMIT_FILTER_TIMEOUT_NANOS=50000000
ENV ENVOY_ADMIN_ADDRESS=0.0.0.0
ENV ENVOY_ADMIN_PORT=8082
ENV ENVOY_XFF_TRUSTED_HOPS=1
ENV ENVOY_HEALTH_CHECK_ENDPOINT=/envoy/healthz

RUN apk upgrade --update-cache \
    && apk add dumb-init \
    && apk add curl \
    && rm -rf /var/cache/apk/*

COPY envtpl /usr/local/bin/
COPY envoy.yaml.tmpl /config/envoy.yaml.tmpl
COPY run.sh /

RUN chmod +x /run.sh

ENTRYPOINT ["/usr/bin/dumb-init", "--"]
CMD ./run.sh
