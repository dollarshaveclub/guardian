# Guardian
[![CircleCI](https://circleci.com/gh/dollarshaveclub/guardian.svg?style=shield&circle-token=79fdec2af5966783fad4b0f4077517ed611394be)](https://circleci.com/gh/dollarshaveclub/guardian)

Guardian is a global rate limiting proxy built on top of [Envoy](https://www.envoyproxy.io/). It supports both synchronous and asynchronous global rate limiting. Synchronous rate limiting waits for a response from Redis before allowing or denying a request. Asynchronous rate limiting uses it's local counts to allow or deny a request and updates its counts with Redis outside of the request hot path. Synchronous rate limiting trades latency for "hard" rate limits. Asynchronous rate limiting has lower latency but can temporarily burst above the configured rate limit. 

Check out [our article introducing Guardian](https://engineering.dollarshaveclub.com/rate-limiting-with-guardian-6520bc84508d) for more detail. 

## Running locally

The docker-compose files contained within `e2e/` can be used to build and run Guardian locally. 

docker-compose-sync.yml runs Guardian in synchronous blocking mode. 
```
docker-compose -f e2e/docker-compose-sync.yml up
```

docker-compose-async.yml runs Guardian in asynchronous blocking mode.
```
docker-compose -f e2e/docker-compose-async.yml up
```

Once running, use the Guardian CLI to set the limit and disable report-only mode. *NOTE* You will see warnings about missing Redis keys in the logs until you do this step.

```
make cli
guardian-cli --redis-address localhost:6379 set-limit 3 1m true # 3 requests per 1 minute, ebaled
guardian-cli --redis-address localhost:6379 set-report-only false # don't just report, actually limit
```

To see rate limiting it in action, use `curl`

```
curl -v localhost:8080/
curl -v localhost:8080/
curl -v localhost:8080/
curl -v localhost:8080/ # This one will be rate limited assuming you used the `set-limit` values from above
```

## Testing

```
go test ./pkg/... # unit tests
make e2e # end to end tests
```
