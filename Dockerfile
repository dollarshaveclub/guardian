FROM golang:1.15
WORKDIR /go/src/github.com/dollarshaveclub/guardian

ARG COMMIT='UNKNOWN'

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go install -ldflags "-w -s -X github.com/dollarshaveclub/guardian/internal/version.Revision=${COMMIT}" github.com/dollarshaveclub/guardian/cmd/guardian
RUN CGO_ENABLED=0 GOOS=linux go install -ldflags "-w -s -X github.com/dollarshaveclub/guardian/internal/version.Revision=${COMMIT}" github.com/dollarshaveclub/guardian/cmd/guardian-cli
RUN CGO_ENABLED=0 GOOS=linux go install -ldflags "-w -s -X github.com/dollarshaveclub/guardian/internal/version.Revision=${COMMIT}" github.com/dollarshaveclub/guardian/cmd/guardian-e2e

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root
COPY --from=0 /go/bin/guardian /bin/
COPY --from=0 /go/bin/guardian-cli /bin/
COPY --from=0 /go/bin/guardian-e2e /bin/
CMD ["/bin/guardian"]
