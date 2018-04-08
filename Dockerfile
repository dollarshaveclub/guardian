FROM golang:1.10
WORKDIR /go/src/github.com/dollarshaveclub/guardian
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go install -ldflags '-w -s' -v github.com/dollarshaveclub/guardian/cmd/guardian

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root

COPY --from=0 /go/bin/guardian /bin/
CMD ["/bin/guardian"]