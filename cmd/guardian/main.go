package main

import (
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/dollarshaveclub/guardian/pkg/guardian"
	"github.com/go-redis/redis"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"gopkg.in/alecthomas/kingpin.v2"
)

func main() {

	logLevel := kingpin.Flag("log-level", "log level.").Short('l').Default("warn").OverrideDefaultFromEnvar("LOG_LEVEL").String()
	address := kingpin.Flag("address", "host:port.").Short('a').Default("0.0.0.0:3000").OverrideDefaultFromEnvar("ADDRESS").String()
	redisAddress := kingpin.Flag("redis-address", "host:port.").Short('r').OverrideDefaultFromEnvar("REDIS_ADDRESS").String()
	redisPoolSize := kingpin.Flag("redis-pool-size", "redis connection pool size").Short('p').Default("20").OverrideDefaultFromEnvar("REDIS_POOL_SIZE").Int()
	dogstatsdAddress := kingpin.Flag("dogstatsd-address", "host:port.").Short('d').OverrideDefaultFromEnvar("DOGSTATSD_ADDRESS").String()
	reportOnly := kingpin.Flag("report-only", "report only, do not block.").Default("false").Short('o').OverrideDefaultFromEnvar("REPORT_ONLY").Bool()
	reqLimit := kingpin.Flag("limit", "request limit per duration.").Short('q').Default("10").OverrideDefaultFromEnvar("LIMIT").Uint64()
	limitDuration := kingpin.Flag("limit-duration", "duration to apply limit. supports time.ParseDuration format.").Short('y').Default("1s").OverrideDefaultFromEnvar("LIMIT_DURATION").Duration()
	limitEnabled := kingpin.Flag("limit-enabled", "rate limit enabled").Short('e').Default("true").OverrideDefaultFromEnvar("LIMIT_ENBALED").Bool()
	confUpdateInterval := kingpin.Flag("conf-update-interval", "interval to fetch new conf from redis").Short('i').Default("10s").OverrideDefaultFromEnvar("CONF_UPDATE_INTERVAL").Duration()
	dogstatsdTags := kingpin.Flag("dogstatsd-tag", "tag to add to dogstatsd metrics").Strings()
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
		ddStatsd, err := statsd.NewBuffered(*dogstatsdAddress, 1000)

		if err != nil {
			logger.WithError(err).Errorf("could create dogstatsd client with address %s", *dogstatsdAddress)
			os.Exit(1)
		}

		ddStatsd.Namespace = "guardian."
		reporter = &guardian.DataDogReporter{Client: ddStatsd, DefaultTags: *dogstatsdTags}
	}

	wg := sync.WaitGroup{}
	redisOpts := &redis.Options{
		Addr:         *redisAddress,
		PoolSize:     *redisPoolSize,
		DialTimeout:  guardian.DefaultRedisDialTimeout,
		ReadTimeout:  guardian.DefaultRedisReadTimeout,
		WriteTimeout: guardian.DefaultRedisWriteTimeout,
	}

	defaultLimit := guardian.Limit{Count: *reqLimit, Duration: *limitDuration, Enabled: *limitEnabled}
	logger.Infof("parsed default limit of %v", defaultLimit)

	logger.Infof("setting up redis client with address of %v and pool size of %v", redisOpts.Addr, redisOpts.PoolSize)
	redis := redis.NewClient(redisOpts)

	redisConfStore := guardian.NewRedisConfStore(redis, defaultLimit, *reportOnly, logger.WithField("context", "redis-conf-provider"))
	logger.Infof("starting cache update for conf store")
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		redisConfStore.RunSync(*confUpdateInterval, stop)
	}()

	whitelister := guardian.NewIPWhitelister(redisConfStore, logger.WithField("context", "ip-whitelister"))

	redisCounter := guardian.NewRedisCounter(redis, logger.WithField("context", "redis-counter"))
	rateLimiter := guardian.NewIPRateLimiter(redisConfStore, redisCounter, logger.WithField("context", "ip-rate-limiter"))

	condWhitelistFunc := guardian.CondStopOnWhitelistFunc(whitelister)
	condRatelimitFunc := guardian.CondStopOnBlockOrError(rateLimiter.Limit)
	condFuncChain := guardian.CondChain(condWhitelistFunc, condRatelimitFunc)

	logger.Infof("starting server on %v", *address)
	server := guardian.NewServer(condFuncChain, redisConfStore, logger.WithField("context", "server"), reporter)

	wg.Add(1)
	go func() {
		defer wg.Done()
		waitGracefulStop(server, stop)
	}()

	err = server.Serve(l)
	if err != nil {
		logger.WithError(err).Error("error running server")
	}

	logger.Info("stopping server")

	redis.Close()
	close(stop)

	wg.Wait()

	logger.Info("goodbye")
	if err != nil {
		os.Exit(1)
	}
}

func waitGracefulStop(server *grpc.Server, stop <-chan struct{}) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-stop:
	case <-sigCh:
	}

	server.GracefulStop()
}
