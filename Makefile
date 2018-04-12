VERSION ?= `git rev-parse HEAD`
.PHONY: docker
docker:
	docker build . -t quay.io/dollarshaveclub/guardian:${VERSION}
	docker push quay.io/dollarshaveclub/guardian:${VERSION}