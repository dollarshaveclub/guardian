package runner

import (
	"io"
	"math/rand"
	"net/http"
	"time"

	pb "github.com/dollarshaveclub/guardian/ratelimit/proto/ratelimit"
	"github.com/dollarshaveclub/guardian/ratelimit/src/config"
	"github.com/dollarshaveclub/guardian/ratelimit/src/redis"
	"github.com/dollarshaveclub/guardian/ratelimit/src/server"
	"github.com/dollarshaveclub/guardian/ratelimit/src/service"
	"github.com/dollarshaveclub/guardian/ratelimit/src/settings"
	logger "github.com/sirupsen/logrus"
)

func Run() {
	srv := server.NewServer("ratelimit", settings.GrpcUnaryInterceptor(nil))

	s := settings.NewSettings()

	level, err := logger.ParseLevel(s.LogLevel)
	if err != nil {
		logger.Errorf("error parsing log level, using default of WARN: %v", err)
		level = logger.WarnLevel
	}

	logger.SetLevel(level)

	service := ratelimit.NewService(
		srv.Runtime(),
		redis.NewRateLimitCacheImpl(
			redis.NewPoolImpl(srv.Scope().Scope("redis_pool")),
			redis.NewTimeSourceImpl(),
			rand.New(redis.NewLockedSource(time.Now().Unix())),
			s.ExpirationJitterMaxSeconds),
		config.NewRateLimitConfigLoaderImpl(),
		srv.Scope().Scope("service"))

	srv.AddDebugHttpEndpoint(
		"/rlconfig",
		"print out the currently loaded configuration for debugging",
		func(writer http.ResponseWriter, request *http.Request) {
			io.WriteString(writer, service.GetCurrentConfig().Dump())
		})

	pb.RegisterRateLimitServiceServer(srv.GrpcServer(), service)
	srv.Start()
}
