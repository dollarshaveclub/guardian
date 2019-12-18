COMMIT   ?= `git rev-parse HEAD`
REPO    ?= github.com/dollarshaveclub/guardian
IMAGE   ?= quay.io/dollarshaveclub/guardian

all: bin

.PHONY: bin
bin:
	go install -ldflags "-X ${REPO}/internal/version.CommitSHA=${COMMIT}" ${REPO}/cmd/guardian

.PHONY: docker
docker:
	docker build . -t ${IMAGE}:${COMMIT} --build-arg COMMIT=${COMMIT}
	docker push ${IMAGE}:${COMMIT}

.PHONY: cli
cli:
	go install -ldflags "-X ${REPO}/internal/version.CommitSHA=${COMMIT}" ${REPO}/cmd/guardian-cli

.PHONY: e2e
e2e: cli
	@./e2e/scripts/run-e2e.sh

.PHONY: e2e-circleci
e2e-circleci: 
	@./e2e/scripts/circleci-run-e2e.sh