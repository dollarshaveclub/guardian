package guardian

import (
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/go-redis/redis"
	"github.com/sirupsen/logrus"
)

const redisIPWhitelistKey = "guardian_conf:whitelist"
const redisLimitCountKey = "guardian_conf:limit_count"
const redisLimitDurationKey = "guardian_conf:limit_duration"
const redisLimitEnabledKey = "guardian_conf:limit_enabled"
const redisReportOnlyKey = "guardian_conf:reportOnly"

// NewRedisConfStore creates a new RedisConfStore
func NewRedisConfStore(redis *redis.Client, defaultLimit Limit, defaultReportOnly bool, logger logrus.FieldLogger) *RedisConfStore {
	defaultConf := conf{whitelist: []net.IPNet{}, limit: defaultLimit, reportOnly: defaultReportOnly}
	return &RedisConfStore{redis: redis, logger: logger, conf: &lockingConf{conf: defaultConf}}
}

// RedisConfStore is a configuration provider that uses Redis for persistence
type RedisConfStore struct {
	redis  *redis.Client
	conf   *lockingConf
	logger logrus.FieldLogger
}

type conf struct {
	whitelist  []net.IPNet
	limit      Limit
	reportOnly bool
}
type lockingConf struct {
	sync.RWMutex
	conf
}

func (rs *RedisConfStore) GetWhitelist() []net.IPNet {
	rs.conf.RLock()
	defer rs.conf.RUnlock()

	return append([]net.IPNet{}, rs.conf.whitelist...)
}

func (rs *RedisConfStore) FetchWhitelist() ([]net.IPNet, error) {
	c := rs.pipelinedFetchConf()
	if c.whitelist == nil {
		return nil, fmt.Errorf("error fetching whitelist")
	}

	return c.whitelist, nil
}

func (rs *RedisConfStore) AddWhitelistCidrs(cidrs []net.IPNet) error {
	key := redisIPWhitelistKey
	for _, cidr := range cidrs {
		field := cidr.String()
		rs.logger.Debugf("Sending HSet for key %v field %v", key, field)
		res := rs.redis.HSet(key, field, "true") // value doesn't matter

		if res.Err() != nil {
			return res.Err()
		}
	}

	return nil
}

func (rs *RedisConfStore) RemoveWhitelistCidrs(cidrs []net.IPNet) error {
	key := redisIPWhitelistKey
	for _, cidr := range cidrs {
		field := cidr.String()
		rs.logger.Debugf("Sending HDel for key %v field %v", key, field)
		res := rs.redis.HDel(key, field, "true") // value doesn't matter

		if res.Err() != nil {
			return res.Err()
		}
	}

	return nil
}

func (rs *RedisConfStore) GetLimit() Limit {
	rs.conf.RLock()
	defer rs.conf.RUnlock()

	return rs.conf.limit
}

func (rs *RedisConfStore) FetchLimit() (Limit, error) {
	c := rs.pipelinedFetchConf()
	if c.limitCount == nil || c.limitDuration == nil || c.limitEnabled == nil {
		return Limit{}, fmt.Errorf("error fetching limit")
	}

	return Limit{Count: *c.limitCount, Duration: *c.limitDuration, Enabled: *c.limitEnabled}, nil
}

func (rs *RedisConfStore) SetLimit(limit Limit) error {
	limitCountStr := strconv.FormatUint(limit.Count, 10)
	limitDurationStr := limit.Duration.String()
	limitEnabledStr := strconv.FormatBool(limit.Enabled)

	pipe := rs.redis.TxPipeline()
	pipe.Set(redisLimitCountKey, limitCountStr, 0)
	pipe.Set(redisLimitDurationKey, limitDurationStr, 0)
	pipe.Set(redisLimitEnabledKey, limitEnabledStr, 0)

	_, err := pipe.Exec()

	return err
}

func (rs *RedisConfStore) GetReportOnly() bool {
	rs.conf.RLock()
	defer rs.conf.RUnlock()

	return rs.conf.reportOnly
}

func (rs *RedisConfStore) FetchReportOnly() (bool, error) {
	c := rs.pipelinedFetchConf()
	if c.reportOnly == nil {
		return false, fmt.Errorf("error fetching report only flag")
	}

	return *c.reportOnly, nil
}

func (rs *RedisConfStore) SetReportOnly(reportOnly bool) error {
	reportOnlyStr := strconv.FormatBool(reportOnly)
	return rs.redis.Set(redisReportOnlyKey, reportOnlyStr, 0).Err()
}

func (rs *RedisConfStore) RunSync(updateInterval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(updateInterval)
	for {
		select {
		case <-ticker.C:
			rs.UpdateCachedConf()
		case <-stop:
			ticker.Stop()
			return
		}
	}
}

func (rs *RedisConfStore) UpdateCachedConf() {
	rs.logger.Debug("Updating conf")

	rs.logger.Debug("Fetching conf")
	fetched := rs.pipelinedFetchConf()
	rs.logger.Debug("Fetched conf: %#v", fetched)

	rs.conf.Lock()
	defer rs.conf.Unlock()

	if fetched.whitelist != nil {
		rs.conf.whitelist = fetched.whitelist
	}

	if fetched.limitCount != nil &&
		fetched.limitDuration != nil &&
		fetched.limitEnabled != nil {
		rs.conf.limit.Count = *fetched.limitCount
		rs.conf.limit.Duration = *fetched.limitDuration
		rs.conf.limit.Enabled = *fetched.limitEnabled
	}

	if fetched.reportOnly != nil {
		rs.conf.reportOnly = *fetched.reportOnly
	}

	rs.logger.Debug("Updated conf")
}

type fetchConf struct {
	whitelist     []net.IPNet
	limitCount    *uint64
	limitDuration *time.Duration
	limitEnabled  *bool
	reportOnly    *bool
}

func (rs *RedisConfStore) pipelinedFetchConf() fetchConf {
	newConf := fetchConf{}
	rs.logger.Debugf("Sending HKEYS for key %v", redisIPWhitelistKey)
	rs.logger.Debugf("Sending GET for key %v", redisLimitCountKey)
	rs.logger.Debugf("Sending GET for key %v", redisLimitDurationKey)
	rs.logger.Debugf("Sending GET for key %v", redisLimitEnabledKey)
	rs.logger.Debugf("Sending GET for key %v", redisReportOnlyKey)

	pipe := rs.redis.Pipeline()
	whitelistKeysCmd := pipe.HKeys(redisIPWhitelistKey)
	limitCountCmd := pipe.Get(redisLimitCountKey)
	limitDurationCmd := pipe.Get(redisLimitDurationKey)
	limitEnabledCmd := pipe.Get(redisLimitEnabledKey)
	reportOnlyCmd := pipe.Get(redisReportOnlyKey)
	pipe.Exec()

	if whitelistStrs, err := whitelistKeysCmd.Result(); err == nil {
		newConf.whitelist = rs.ipNetsFromStrings(whitelistStrs)
	} else {
		rs.logger.WithError(err).Warnf("error send HKEYS for key %v", redisIPWhitelistKey)
	}

	if limitCount, err := limitCountCmd.Uint64(); err == nil {
		newConf.limitCount = &limitCount
	} else {
		rs.logger.WithError(err).Warnf("error sending GET for key %v", redisLimitCountKey)
	}

	if limitDurationStr, err := limitDurationCmd.Result(); err == nil {
		limitDuration, err := time.ParseDuration(limitDurationStr)
		if err != nil {
			rs.logger.WithError(err).Warnf("error parsing limit duration")
		} else {
			newConf.limitDuration = &limitDuration
		}
	} else {
		rs.logger.WithError(err).Errorf("error sending GET for key %v", redisLimitDurationKey)
	}

	if limitEnabledStr, err := limitEnabledCmd.Result(); err == nil {
		limitEnabled, err := strconv.ParseBool(limitEnabledStr)
		if err != nil {
			rs.logger.WithError(err).Warnf("error parsing limit enabled")
		} else {
			newConf.limitEnabled = &limitEnabled
		}
	} else {
		rs.logger.WithError(err).Errorf("error sending GET for key %v", redisLimitEnabledKey)
	}

	if reportOnlyStr, err := reportOnlyCmd.Result(); err == nil {
		reportOnly, err := strconv.ParseBool(reportOnlyStr)
		if err != nil {
			rs.logger.WithError(err).Warnf("error parsing report only")
		} else {
			newConf.reportOnly = &reportOnly
		}
	} else {
		rs.logger.WithError(err).Warnf("error sending GET for key %v", redisReportOnlyKey)

	}

	return newConf
}

func (rs *RedisConfStore) ipNetsFromStrings(ipNetStrs []string) []net.IPNet {
	ipNets := []net.IPNet{}
	for _, cidrString := range ipNetStrs {
		_, cidr, err := net.ParseCIDR(cidrString)
		if err != nil {
			rs.logger.WithError(err).Warnf("error parsing cidr from %v", cidrString)
			continue
		}

		ipNets = append(ipNets, *cidr)
	}

	return ipNets
}
