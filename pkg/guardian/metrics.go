package guardian

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/sirupsen/logrus"
)

const durationMetricName = "request.duration"
const reqWhitelistMetricName = "request.whitelist"
const reqBlacklistMetricName = "request.blacklist"
const reqJailMetricName = "request.jails"
const addPrisonerMetricName = "prisoners.add"
const reqRateLimitMetricName = "request.rate_limit"
const redisCounterIncrMetricName = "redis_counter.incr"
const redisCounterPrunedMetricName = "redis_counter.cache.pruned"
const redisCounterCacheSizeMetricName = "redis_counter.cache.size"
const redisCounterPrunePassMetricName = "redis_counter.cache.prune_pass"
const redisObtainLockMetricName = "redis.obtain_lock"
const rateLimitCountMetricName = "rate_limit.count"
const rateLimitDurationMetricName = "rate_limit.duration"
const rateLimitEnabledMetricName = "rate_limit.enabled"
const routeRateLimitMetricName = "route_rate_limit.count"
const jailMetricName = "jail.count"
const whitelistCountMetricName = "whitelist.count"
const blacklistCountMetricName = "blacklist.count"
const prisonersCountMetricName = "prisoners.count"
const reportOnlyEnabledMetricName = "report_only.enabled"
const blockedKey = "blocked"
const whitelistedKey = "whitelisted"
const blacklistedKey = "blacklisted"
const ratelimitedKey = "ratelimited"
const errorKey = "error"
const durationKey = "duration"
const routeKey = "route"
const enabledKey = "enabled"
const banDurationKey = "ban_duration"
const remoteAddressKey = "remote_address"
const jailKey = "jail"

const metricChannelBuffSize = 1000000

type MetricReporter interface {
	Duration(request Request, blocked bool, errorOccurred bool, duration time.Duration)
	HandledWhitelist(request Request, whitelisted bool, errorOccurred bool, duration time.Duration)
	HandledBlacklist(request Request, whitelisted bool, errorOccurred bool, duration time.Duration)
	HandledRatelimit(request Request, ratelimited bool, errorOccurred bool, duration time.Duration)
	HandledRatelimitWithRoute(request Request, ratelimited bool, errorOccurred bool, duration time.Duration)
	HandledJail(request Request, blocked bool, errorOccurred bool, duration time.Duration, setters ...MetricOptionSetter)
	HandledAddPrisoner(ip net.IP, jail Jail)
	RedisCounterIncr(duration time.Duration, errorOccurred bool)
	RedisCounterPruned(duration time.Duration, cacheSize float64, prunedCounted float64)
	RedisObtainLock(duration time.Duration, errorOccurred bool)
	CurrentGlobalLimit(limit Limit)
	CurrentRouteLimit(route string, limit Limit)
	CurrentWhitelist(whitelist []net.IPNet)
	CurrentBlacklist(blacklist []net.IPNet)
	CurrentRouteJail(route string, jail Jail)
	CurrentPrisoners(numPrisoners int)
	CurrentReportOnlyMode(reportOnly bool)
}

type DataDogReporter struct {
	client      *statsd.Client
	logger      logrus.FieldLogger
	defaultTags []string
	c           chan func()
}

type MetricOption struct {
	additionalTags []string
}

type MetricOptionSetter func(mo *MetricOption)

func WithRemoteAddress(remoteAddress string) MetricOptionSetter {
	return func(mo *MetricOption) {
		mo.additionalTags = append(mo.additionalTags, remoteAddressKey + ":" + remoteAddress)
	}
}

func NewDataDogReporter(client *statsd.Client, defaultTags []string, logger logrus.FieldLogger) *DataDogReporter {
	return &DataDogReporter{
		client:      client,
		logger:      logger,
		defaultTags: defaultTags,
		c:           make(chan func(), metricChannelBuffSize),
	}
}

func (d *DataDogReporter) Run(stop <-chan struct{}) {
	for {
		select {
		case f := <-d.c:
			f()
		case <-stop:
			for f := range d.c { // drain the channel
				f()
			}
			return
		}
	}
}

func (d *DataDogReporter) Duration(request Request, blocked bool, errorOccurred bool, duration time.Duration) {
	f := func() {
		blockedTag := blockedKey + ":" + strconv.FormatBool(blocked)
		errorTag := errorKey + ":" + strconv.FormatBool(errorOccurred)
		tags := append([]string{blockedTag, errorTag}, d.defaultTags...)
		d.client.TimeInMilliseconds(durationMetricName, float64(duration/time.Millisecond), tags, 1)
	}

	d.enqueue(f)
}

func (d *DataDogReporter) HandledWhitelist(request Request, whitelisted bool, errorOccurred bool, duration time.Duration) {
	f := func() {
		whitelistedTag := whitelistedKey + ":" + strconv.FormatBool(whitelisted)
		errorTag := errorKey + ":" + strconv.FormatBool(errorOccurred)
		tags := append([]string{whitelistedTag, errorTag}, d.defaultTags...)
		d.client.TimeInMilliseconds(reqWhitelistMetricName, float64(duration/time.Millisecond), tags, 1.0)
	}
	d.enqueue(f)
}

func (d *DataDogReporter) HandledBlacklist(request Request, blacklisted bool, errorOccurred bool, duration time.Duration) {
	f := func() {
		blacklistedTag := blacklistedKey + ":" + strconv.FormatBool(blacklisted)
		errorTag := errorKey + ":" + strconv.FormatBool(errorOccurred)
		tags := append([]string{blacklistedTag, errorTag}, d.defaultTags...)
		d.client.TimeInMilliseconds(reqBlacklistMetricName, float64(duration/time.Millisecond), tags, 1.0)
	}
	d.enqueue(f)
}

func (d *DataDogReporter) HandledRatelimit(request Request, ratelimited bool, errorOccurred bool, duration time.Duration) {
	f := func() {
		ratelimitedTag := ratelimitedKey + ":" + strconv.FormatBool(ratelimited)
		errorTag := errorKey + ":" + strconv.FormatBool(errorOccurred)
		tags := append([]string{ratelimitedTag, errorTag}, d.defaultTags...)
		d.client.TimeInMilliseconds(reqRateLimitMetricName, float64(duration/time.Millisecond), tags, 1.0)
	}
	d.enqueue(f)
}

func (d *DataDogReporter) HandledRatelimitWithRoute(request Request, ratelimited bool, errorOccurred bool, duration time.Duration) {
	f := func() {
		ratelimitedTag := ratelimitedKey + ":" + strconv.FormatBool(ratelimited)
		errorTag := errorKey + ":" + strconv.FormatBool(errorOccurred)
		routeTag := routeKey + ":" + request.Path
		tags := append([]string{ratelimitedTag, errorTag, routeTag}, d.defaultTags...)
		d.client.TimeInMilliseconds(reqRateLimitMetricName, float64(duration/time.Millisecond), tags, 1.0)
	}
	d.enqueue(f)
}

func (d *DataDogReporter) HandledJail(request Request, blocked bool, errorOccurred bool, duration time.Duration, setters ...MetricOptionSetter) {
	mo := MetricOption{}
	for _, fn := range setters {
		fn(&mo)
	}
	tags := append([]string{}, d.defaultTags...)
	tags = append(tags, mo.additionalTags...)
	f := func() {
		blockedTag := blockedKey + ":" + strconv.FormatBool(blocked)
		errorTag := errorKey + ":" + strconv.FormatBool(errorOccurred)
		tags := append(tags, []string{blockedTag, errorTag}...)
		d.client.TimeInMilliseconds(reqJailMetricName, float64(duration/time.Millisecond), tags, 1.0)
	}
	d.enqueue(f)
}

func (d *DataDogReporter) HandledAddPrisoner(ip net.IP, jail Jail) {
	tags := append([]string{}, d.defaultTags...)
	f := func() {
		remoteAddressTag := remoteAddressKey + ":" + ip.String()
		banDurationTag := banDurationKey + ":" + jail.BanDuration.String()
		tags = append(tags, []string{remoteAddressTag, banDurationTag}...)
		d.client.Count(addPrisonerMetricName, 1, tags, 1.0)
	}
	d.enqueue(f)
}

func (d *DataDogReporter) RedisCounterIncr(duration time.Duration, errorOccurred bool) {
	f := func() {
		errorTag := errorKey + ":" + strconv.FormatBool(errorOccurred)
		tags := append([]string{errorTag}, d.defaultTags...)
		d.client.TimeInMilliseconds(redisCounterIncrMetricName, float64(duration/time.Millisecond), tags, 1.0)
	}
	d.enqueue(f)
}

func (d *DataDogReporter) RedisCounterPruned(duration time.Duration, cacheSize float64, prunedCounted float64) {
	f := func() {
		d.client.Gauge(redisCounterCacheSizeMetricName, cacheSize, d.defaultTags, 1)
		d.client.Gauge(redisCounterPrunedMetricName, prunedCounted, d.defaultTags, 1)
		d.client.TimeInMilliseconds(redisCounterPrunePassMetricName, float64(duration/time.Millisecond), d.defaultTags, 1)
	}
	d.enqueue(f)
}


func (d *DataDogReporter) RedisObtainLock(duration time.Duration, errorOccurred bool) {
	f := func() {
		errorTag := errorKey + ":" + strconv.FormatBool(errorOccurred)
		tags := append([]string{errorTag}, d.defaultTags...)
		d.client.Timing(redisObtainLockMetricName, duration, tags, 1.0)
	}
	d.enqueue(f)
}

func (d *DataDogReporter) CurrentGlobalLimit(limit Limit) {
	f := func() {
		enabled := 0
		if limit.Enabled {
			enabled = 1
		}
		d.client.Gauge(rateLimitCountMetricName, float64(limit.Count), d.defaultTags, 1)
		d.client.Gauge(rateLimitDurationMetricName, float64(limit.Duration), d.defaultTags, 1)
		d.client.Gauge(rateLimitEnabledMetricName, float64(enabled), d.defaultTags, 1)
	}
	d.enqueue(f)
}

func (d *DataDogReporter) CurrentRouteLimit(route string, limit Limit) {
	f := func() {
		rk := routeKey + ":" + route
		dk := durationKey + ":" + limit.Duration.String()
		ek := enabledKey + ":" + fmt.Sprintf("%v", limit.Enabled)
		d.client.Gauge(routeRateLimitMetricName, float64(limit.Count), append(d.defaultTags, rk, dk, ek), 1)
	}
	d.enqueue(f)
}

func (d *DataDogReporter) CurrentRouteJail(route string, jail Jail) {
	f := func() {
		rk := routeKey + ":" + route
		dk := durationKey + ":" + jail.Limit.Duration.String()
		ek := enabledKey + ":" + strconv.FormatBool(jail.Limit.Enabled)
		bdk := banDurationKey + ":" + jail.BanDuration.String()
		d.client.Gauge(jailMetricName, float64(jail.Limit.Count), append(d.defaultTags, rk, dk, ek, bdk), 1)
	}
	d.enqueue(f)
}

func (d *DataDogReporter) CurrentWhitelist(whitelist []net.IPNet) {
	f := func() {
		d.client.Gauge(whitelistCountMetricName, float64(len(whitelist)), d.defaultTags, 1)
	}
	d.enqueue(f)
}

func (d *DataDogReporter) CurrentBlacklist(blacklist []net.IPNet) {
	f := func() {
		d.client.Gauge(blacklistCountMetricName, float64(len(blacklist)), d.defaultTags, 1)
	}
	d.enqueue(f)
}

func (d *DataDogReporter) CurrentPrisoners(numPrisoners int ) {
	f := func() {
		d.client.Gauge(prisonersCountMetricName, float64(numPrisoners), d.defaultTags, 1)
	}
	d.enqueue(f)
}

func (d *DataDogReporter) CurrentReportOnlyMode(reportOnly bool) {
	f := func() {
		enabled := 0
		if reportOnly {
			enabled = 1
		}
		d.client.Gauge(reportOnlyEnabledMetricName, float64(enabled), d.defaultTags, 1)
	}
	d.enqueue(f)
}

func (d *DataDogReporter) enqueue(f func()) {
	select {
	case d.c <- f:
	default:
		d.logger.Error("buffered channel full -- discarding metric")
	}
}

type NullReporter struct{}

func (n NullReporter) Duration(request Request, blocked bool, errorOccurred bool, duration time.Duration) {
}

func (n NullReporter) HandledWhitelist(request Request, whitelisted bool, errorOccurred bool, duration time.Duration) {
}

func (n NullReporter) HandledBlacklist(request Request, blacklisted bool, errorOccurred bool, duration time.Duration) {

}

func (n NullReporter) HandledRatelimit(request Request, ratelimited bool, errorOccurred bool, duration time.Duration) {
}

func (n NullReporter) HandledRatelimitWithRoute(request Request, ratelimited bool, errorOccurred bool, duration time.Duration) {
}

func (n NullReporter)  HandledJail(request Request, blocked bool, errorOccurred bool, duration time.Duration, setters ...MetricOptionSetter) {
}

func (n NullReporter) HandledAddPrisoner(ip net.IP, jail Jail) {
}

func (n NullReporter) RedisCounterIncr(duration time.Duration, errorOccurred bool) {
}

func (n NullReporter) RedisCounterPruned(duration time.Duration, cacheSize float64, prunedCounted float64) {
}

func (n NullReporter)  RedisObtainLock(duration time.Duration, errorOccurred bool) {
}

func (n NullReporter) CurrentGlobalLimit(limit Limit) {
}

func (n NullReporter) CurrentRouteLimit(route string, limit Limit) {
}

func (n NullReporter) CurrentRouteJail(route string, jail Jail) {
}

func (n NullReporter) CurrentWhitelist(whitelist []net.IPNet) {
}

func (n NullReporter) CurrentBlacklist(blacklist []net.IPNet) {
}

func (n NullReporter) CurrentPrisoners(numPrisoners int) {
}

func (n NullReporter) CurrentReportOnlyMode(reportOnly bool) {
}
