FROM golang:1.12-alpine
WORKDIR /go/src/github.com/dollarshaveclub/guardian

ARG COMMIT='UNKNOWN'

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go install -ldflags "-w -s -X github.com/dollarshaveclub/guardian/internal/version.Revision=${COMMIT}" github.com/dollarshaveclub/guardian/cmd/guardian

RUN apk --no-cache add ca-certificates
ENV GO111MODULE=on
RUN apk add git
RUN mkdir -p /go/src/bradleyjkemp
WORKDIR /go/src/bradleyjkemp
RUN git clone https://github.com/bradleyjkemp/grpc-tools
WORKDIR /go/src/bradleyjkemp/grpc-tools/grpc-dump
RUN go install github.com/bradleyjkemp/grpc-tools/grpc-dump
CMD ["/go/bin/grpc-dump", "--port=3000"]
