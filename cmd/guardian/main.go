package main

import (
	"net"
	"os"
	"time"

	"github.com/dollarshaveclub/guardian/pkg/guardian"
	"github.com/sirupsen/logrus"
)

func main() {

	logger := logrus.StandardLogger()
	level, err := logrus.ParseLevel(os.Getenv("LOG_LEVEL"))
	if err != nil {
		level = logrus.ErrorLevel
	}

	logger.Warnf("setting log level to %v", level)
	logger.SetLevel(level)

	addr := "0.0.0.0:8081"
	l, err := net.Listen("tcp", addr)

	if err != nil {
		logger.WithError(err).Errorf("could not listen on %s", addr)
		os.Exit(1)
	}

	limit := guardian.Limit{Count: 10, Duration: 1 * time.Second, Enabled: true}
	redisOpts := guardian.RedisPoolOpts{Addr: "redis:6379"}
	redis := guardian.NewRedisLimitStore(limit, redisOpts, logger.WithField("context", "redis"))
	rateLimiter := guardian.NewIPRateLimiter(redis, logger.WithField("context", "ip-rate-limiter"))
	server := guardian.NewServer(rateLimiter.Limit, false, logger.WithField("context", "server"))

	logger.Infof("starting server on %v", addr)
	err = server.Serve(l)
	if err != nil {
		logger.WithError(err).Error("error running server")
		os.Exit(1)
	}
}
