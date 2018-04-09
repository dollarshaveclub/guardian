package main

import (
	"net"
	"os"
	"time"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/dollarshaveclub/guardian/pkg/guardian"
	"github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
)

// DefaultRedisReadTimeout is the default timeout used when reading a reply from redis
var DefaultRedisReadTimeout = 100 * time.Millisecond

// DefaultRedisWriteTimeout is the default timeout used when writing to redis
var DefaultRedisWriteTimeout = 100 * time.Millisecond

func main() {

	logLevel := kingpin.Flag("log-level", "log level.").Short('l').Default("warn").OverrideDefaultFromEnvar("LOG_LEVEL").String()
	address := kingpin.Flag("address", "host:port.").Short('a').Default("0.0.0.0:3000").OverrideDefaultFromEnvar("ADDRESS").String()
	redisAddress := kingpin.Flag("redis-address", "host:port.").Short('r').OverrideDefaultFromEnvar("REDIS_ADDRESS").String()
	dogstatsdAddress := kingpin.Flag("dogstatsd-address", "host:port.").Short('d').OverrideDefaultFromEnvar("DOGSTATSD_ADDRESS").String()
	reportOnly := kingpin.Flag("report-only", "report only, do not block.").Default("false").Short('o').OverrideDefaultFromEnvar("REPORT_ONLY").Bool()
	reqLimit := kingpin.Flag("limit", "request limit per duration.").Short('q').Default("10").OverrideDefaultFromEnvar("LIMIT").Uint64()
	limitDuration := kingpin.Flag("limit-duration", "duration to apply limit. supports time.ParseDuration format.").Short('y').Default("1s").OverrideDefaultFromEnvar("LIMIT_DURATION").Duration()
	kingpin.Parse()

	logger := logrus.StandardLogger()
	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		level = logrus.ErrorLevel
	}

	logger.Warnf("setting log level to %v", level)
	logger.SetLevel(level)

	l, err := net.Listen("tcp", *address)
	if err != nil {
		logger.WithError(err).Errorf("could not listen on %s", *address)
		os.Exit(1)
	}

	var reporter guardian.MetricReporter
	if len(*dogstatsdAddress) == 0 {
		reporter = guardian.NullReporter{}
	} else {
		ddStatsd, err := statsd.NewBuffered(*dogstatsdAddress, 100)
		if err != nil {
			logger.WithError(err).Errorf("could create dogstatsd client with address %s", *dogstatsdAddress)
			os.Exit(1)
		}

		ddStatsd.Namespace = "guardian."
		reporter = &guardian.DataDogReporter{Client: ddStatsd}
	}

	limit := guardian.Limit{Count: *reqLimit, Duration: *limitDuration, Enabled: true}
	redisOpts := guardian.RedisPoolOpts{Addr: *redisAddress}

	logger.Infof("setting ip rate limiter to use redis store with %v", limit)

	redis := guardian.NewRedisLimitStore(limit, redisOpts, DefaultRedisReadTimeout, DefaultRedisWriteTimeout, logger.WithField("context", "redis"))
	rateLimiter := guardian.NewIPRateLimiter(redis, logger.WithField("context", "ip-rate-limiter"))

	logger.Infof("starting server on %v", *address)
	server := guardian.NewServer(rateLimiter.Limit, *reportOnly, logger.WithField("context", "server"), reporter)
	err = server.Serve(l)
	if err != nil {
		logger.WithError(err).Error("error running server")
		os.Exit(1)
	}
}
