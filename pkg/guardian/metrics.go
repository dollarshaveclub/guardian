package guardian

import (
	"net"
	"strconv"
	"time"

	"github.com/go-kit/kit/metrics"
	"github.com/go-kit/kit/metrics/dogstatsd"
)

type MetricReporter interface {
	Duration(request Request, blocked bool, errorOccurred bool, duration time.Duration)
	HandledWhitelist(request Request, whitelisted bool, errorOccurred bool, duration time.Duration)
	HandledRatelimit(request Request, ratelimited bool, errorOccurred bool, duration time.Duration)
	RedisCounterIncr(duration time.Duration, errorOccurred bool)
	RedisCounterPruned(duration time.Duration, cacheSize float64, prunedCounted float64)
	CurrentLimit(limit Limit)
	CurrentWhitelist(whitelist []net.IPNet)
	CurrentReportOnlyMode(reportOnly bool)
}

type DataDogReporter struct {
	reqDuration            metrics.Histogram
	reqWhitelist           metrics.Histogram
	reqRatelimit           metrics.Histogram
	redisCounterIncr       metrics.Histogram
	redisCounterCacheSize  metrics.Gauge
	redisCounterPruned     metrics.Gauge
	redisCounterPrunedPass metrics.Histogram
	ratelimitCount         metrics.Gauge
	ratelimitDuration      metrics.Gauge
	ratelimitEnabled       metrics.Gauge
	whitelistLen           metrics.Gauge
	reportOnly             metrics.Gauge
}

func NewDataDogReporter(client *dogstatsd.Dogstatsd) *DataDogReporter {
	d := &DataDogReporter{}
	d.reqDuration = client.NewTiming(durationMetricName, 1.0)
	d.reqWhitelist = client.NewTiming(reqWhitelistMetricName, 1.0)
	d.reqRatelimit = client.NewTiming(reqRateLimitMetricName, 1.0)
	d.redisCounterIncr = client.NewTiming(redisCounterIncrMetricName, 1.0)
	d.redisCounterCacheSize = client.NewGauge(redisCounterCacheSizeMetricName)
	d.redisCounterPruned = client.NewGauge(redisCounterPrunedMetricName)
	d.redisCounterPrunedPass = client.NewTiming(redisCounterPrunePassMetricName, 1.0)
	d.ratelimitCount = client.NewGauge(rateLimitCountMetricName)
	d.ratelimitDuration = client.NewGauge(rateLimitDurationMetricName)
	d.ratelimitEnabled = client.NewGauge(rateLimitEnabledMetricName)
	d.whitelistLen = client.NewGauge(whitelistCountMetricName)
	d.reportOnly = client.NewGauge(reportOnlyEnabledMetricName)

	return d
}

const durationMetricName = "request.duration"
const reqWhitelistMetricName = "request.whitelist"
const reqRateLimitMetricName = "request.rate_limit"
const redisCounterIncrMetricName = "redis_counter.incr"
const redisCounterPrunedMetricName = "redis_counter.cache.pruned"
const redisCounterCacheSizeMetricName = "redis_counter.cache.size"
const redisCounterPrunePassMetricName = "redis_counter.cache.prune_pass"
const rateLimitCountMetricName = "rate_limit.count"
const rateLimitDurationMetricName = "rate_limit.duration"
const rateLimitEnabledMetricName = "rate_limit.enabled"
const whitelistCountMetricName = "whitelist.count"

const reportOnlyEnabledMetricName = "report_only.enabled"
const blockedKey = "blocked"
const whitelistedKey = "whitelisted"
const ratelimitedKey = "ratelimited"
const errorKey = "error"
const authorityKey = "authority"
const ingressClassKey = "ingress_class"

func (d *DataDogReporter) Duration(request Request, blocked bool, errorOccurred bool, duration time.Duration) {
	tags := []string{authorityKey, request.Authority, blockedKey, strconv.FormatBool(blocked), errorKey, strconv.FormatBool(errorOccurred)}
	d.reqDuration.With(tags...).Observe(float64(duration / time.Millisecond))
}

func (d *DataDogReporter) HandledWhitelist(request Request, whitelisted bool, errorOccurred bool, duration time.Duration) {
	tags := []string{authorityKey, request.Authority, whitelistedKey, strconv.FormatBool(whitelisted), errorKey, strconv.FormatBool(errorOccurred)}
	d.reqWhitelist.With(tags...).Observe(float64(duration / time.Millisecond))
}

func (d *DataDogReporter) HandledRatelimit(request Request, ratelimited bool, errorOccurred bool, duration time.Duration) {
	tags := []string{authorityKey, request.Authority, ratelimitedKey, strconv.FormatBool(ratelimited), errorKey, strconv.FormatBool(errorOccurred)}
	d.reqRatelimit.With(tags...).Observe(float64(duration / time.Millisecond))
}

func (d *DataDogReporter) RedisCounterIncr(duration time.Duration, errorOccurred bool) {
	tags := []string{errorKey, strconv.FormatBool(errorOccurred)}
	d.redisCounterIncr.With(tags...).Observe(float64(duration / time.Millisecond))
}

func (d *DataDogReporter) RedisCounterPruned(duration time.Duration, cacheSize float64, prunedCounted float64) {
	d.redisCounterPrunedPass.Observe(float64(duration / time.Millisecond))
	d.redisCounterCacheSize.Set(cacheSize)
	d.redisCounterPruned.Set(prunedCounted)
}

func (d *DataDogReporter) CurrentLimit(limit Limit) {
	enabled := 0
	if limit.Enabled {
		enabled = 1
	}

	d.ratelimitCount.Set(float64(limit.Count))
	d.ratelimitDuration.Set(float64(limit.Duration))
	d.ratelimitEnabled.Set(float64(enabled))
}

func (d *DataDogReporter) CurrentWhitelist(whitelist []net.IPNet) {
	d.whitelistLen.Set(float64(len(whitelist)))
}

func (d *DataDogReporter) CurrentReportOnlyMode(reportOnly bool) {
	enabled := 0
	if reportOnly {
		enabled = 1
	}
	d.reportOnly.Set(float64(enabled))
}

type NullReporter struct{}

func (n NullReporter) Duration(request Request, blocked bool, errorOccured bool, duration time.Duration) {
}

func (n NullReporter) HandledWhitelist(request Request, whitelisted bool, errorOccured bool, duration time.Duration) {
}

func (n NullReporter) HandledRatelimit(request Request, ratelimited bool, errorOccured bool, duration time.Duration) {
}

func (n NullReporter) RedisCounterIncr(duration time.Duration, errorOccurred bool) {
}
func (n NullReporter) RedisCounterPruned(duration time.Duration, cacheSize float64, prunedCounted float64) {
}

func (n NullReporter) CurrentLimit(limit Limit) {
}

func (n NullReporter) CurrentWhitelist(whitelist []net.IPNet) {
}

func (n NullReporter) CurrentReportOnlyMode(reportOnly bool) {
}
