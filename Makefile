.PHONY: docker
docker:
	docker build docker/ --file docker/Dockerfile-dsc-ratelimit -t quay.io/dollarshaveclub/dsc-ratelimit:`git rev-parse HEAD`
	docker build docker/ --file docker/Dockerfile-dsc-envoy -t quay.io/dollarshaveclub/dsc-envoy:`git rev-parse HEAD`
	docker push quay.io/dollarshaveclub/dsc-ratelimit:`git rev-parse HEAD`
	docker push quay.io/dollarshaveclub/dsc-envoy:`git rev-parse HEAD`
