package main

import (
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	_ "net/http/pprof"

	"cloud.google.com/go/profiler"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/dollarshaveclub/guardian/internal/version"
	"github.com/dollarshaveclub/guardian/pkg/guardian"
	"github.com/dollarshaveclub/guardian/pkg/rate_limit_grpc"
	"github.com/go-redis/redis"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"gopkg.in/alecthomas/kingpin.v2"
)

func main() {

	logLevel := kingpin.Flag("log-level", "log level.").Short('l').Default("warn").OverrideDefaultFromEnvar("GUARDIAN_FLAG_LOG_LEVEL").String()
	address := kingpin.Flag("address", "network address to listen on.").Short('a').Default("0.0.0.0:3000").OverrideDefaultFromEnvar("GUARDIAN_FLAG_ADDRESS").String()
	network := kingpin.Flag("network", "network to listen on. Must be \"tcp\", \"tcp4\", \"tcp6\", \"unix\" or \"unixpacket\".").Short('n').Default("tcp").OverrideDefaultFromEnvar("GUARDIAN_FLAG_NETWORK").String()
	redisAddress := kingpin.Flag("redis-address", "host:port.").Short('r').OverrideDefaultFromEnvar("GUARDIAN_FLAG_REDIS_ADDRESS").String()
	redisPoolSize := kingpin.Flag("redis-pool-size", "redis connection pool size").Short('p').Default("20").OverrideDefaultFromEnvar("GUARDIAN_FLAG_REDIS_POOL_SIZE").Int()
	dogstatsdAddress := kingpin.Flag("dogstatsd-address", "host:port.").Short('d').OverrideDefaultFromEnvar("GUARDIAN_FLAG_DOGSTATSD_ADDRESS").String()
	reportOnly := kingpin.Flag("report-only", "report only, do not block.").Default("false").Short('o').OverrideDefaultFromEnvar("GUARDIAN_FLAG_REPORT_ONLY").Bool()
	reqLimit := kingpin.Flag("limit", "request limit per duration.").Short('q').Default("10").OverrideDefaultFromEnvar("GUARDIAN_FLAG_LIMIT").Uint64()
	limitDuration := kingpin.Flag("limit-duration", "duration to apply limit. supports time.ParseDuration format.").Short('y').Default("1s").OverrideDefaultFromEnvar("GUARDIAN_FLAG_LIMIT_DURATION").Duration()
	limitEnabled := kingpin.Flag("limit-enabled", "rate limit enabled").Short('e').Default("true").OverrideDefaultFromEnvar("GUARDIAN_FLAG_LIMIT_ENABLED").Bool()
	confUpdateInterval := kingpin.Flag("conf-update-interval", "interval to fetch new conf from redis").Short('i').Default("10s").OverrideDefaultFromEnvar("GUARDIAN_FLAG_CONF_UPDATE_INTERVAL").Duration()
	dogstatsdTags := kingpin.Flag("dogstatsd-tag", "tag to add to dogstatsd metrics").Strings()
	defaultWhitelist := kingpin.Flag("whitelist-cidr", "default cidr to whitelist until sync with redis occurs").Strings()
	defaultBlacklist := kingpin.Flag("blacklist-cidr", "default cidr to blacklist until sync with redis occurs").Strings()
	profilerEnabled := kingpin.Flag("profiler-enabled", "GCP Stackdriver Profiler enabled").Default("false").OverrideDefaultFromEnvar("GUARDIAN_FLAG_PROFILER_ENABLED").Bool()
	profilerProjectID := kingpin.Flag("profiler-project-id", "GCP Stackdriver Profiler project ID").OverrideDefaultFromEnvar("GUARDIAN_FLAG_PROFILER_PROJECT_ID").String()
	profilerServiceName := kingpin.Flag("profiler-service-name", "GCP Stackdriver Profiler service name").Default("guardian").OverrideDefaultFromEnvar("GUARDIAN_FLAG_PROFILER_SERVICE_NAME").String()
	synchronous := kingpin.Flag("synchronous", "synchronously enforce ratelimit").Default("false").OverrideDefaultFromEnvar("GUARDIAN_FLAG_SYNCHRONOUS").Bool()
	kingpin.Parse()

	logger := logrus.StandardLogger()
	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		level = logrus.ErrorLevel
	}

	logger.Warnf("setting log level to %v", level)
	logger.SetLevel(level)

	l, err := net.Listen(*network, *address)
	if err != nil {
		logger.WithError(err).Errorf("could not listen on network %s address %s", *network, *address)
		os.Exit(1)
	}

	stop := make(chan struct{})

	wg := sync.WaitGroup{}
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
		ddReporter := guardian.NewDataDogReporter(ddStatsd, *dogstatsdTags, logger.WithField("context", "datadog-metric-reporter"))
		wg.Add(1)
		go func() {
			defer wg.Done()
			ddReporter.Run(stop)
		}()
		reporter = ddReporter
	}

	defaultLimit := guardian.Limit{Count: *reqLimit, Duration: *limitDuration, Enabled: *limitEnabled}
	logger.Infof("parsed default limit of %v", defaultLimit)

	redisOpts := &redis.Options{
		Addr:     *redisAddress,
		PoolSize: *redisPoolSize,
	}

	logger.Infof("setting up redis client with address of %v and pool size of %v", redisOpts.Addr, redisOpts.PoolSize)
	redis := redis.NewClient(redisOpts)

	redisConfStore := guardian.NewRedisConfStore(redis, guardian.IPNetsFromStrings(*defaultWhitelist, logger), guardian.IPNetsFromStrings(*defaultBlacklist, logger), defaultLimit, *reportOnly, logger.WithField("context", "redis-conf-provider"), reporter)
	logger.Infof("starting cache update for conf store")

	wg.Add(1)
	go func() {
		defer wg.Done()
		redisConfStore.RunSync(*confUpdateInterval, stop)
	}()

	redisCounter := guardian.NewRedisCounter(redis, *synchronous, logger.WithField("context", "redis-counter"), reporter)
	wg.Add(1)
	go func() {
		defer wg.Done()
		redisCounter.Run(30*time.Second, stop)
	}()

	whitelister := guardian.NewIPWhitelister(redisConfStore, logger.WithField("context", "ip-whitelister"), reporter)
	blacklister := guardian.NewIPBlacklister(redisConfStore, logger.WithField("context", "ip-blacklister"), reporter)
	ipRateLimiter := guardian.NewIPRateLimiter(redisConfStore, logger.WithField("context", "ip-rate-limiter"), reporter, redisCounter)
	routeRateLimiter := guardian.NewRouteRateLimiter(redisConfStore, logger.WithField("context", "route-rate-limiter"), reporter, redisCounter)
	condFuncChain := guardian.DefaultCondChain(whitelister, blacklister, ipRateLimiter, routeRateLimiter)

	logger.Infof("starting server on %v", *address)
	server := guardian.NewServer(condFuncChain, redisConfStore, logger.WithField("context", "server"), reporter)
	grpcServer := rate_limit_grpc.NewRateLimitServer(server)

	wg.Add(1)
	go func() {
		defer wg.Done()
		waitGracefulStop(grpcServer, stop)
	}()

	if *profilerEnabled {
		config := profiler.Config{
			Service:        *profilerServiceName,
			ServiceVersion: version.Revision,
			ProjectID:      *profilerProjectID,
			MutexProfiling: true,
		}
		if err := profiler.Start(config); err != nil {
			logger.WithError(err).Error("cannot start the profiler")
		}
	}

	err = grpcServer.Serve(l)
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
