package guardian

import (
	"net"
	"sync"
	"time"

	"github.com/go-redis/redis"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const ipWhitelistStoreNamespace = "ip_whitelist_store"
const ipWhitelistKey = "whitelist"

// NewRedisIPWhitelistStore creates a new RedisIPWhitelistStore
func NewRedisIPWhitelistStore(redis *redis.Client, logger logrus.FieldLogger) *RedisIPWhitelistStore {
	return &RedisIPWhitelistStore{redis: redis, logger: logger, cache: lockingCache{c: []net.IPNet{}}}
}

// RedisIPWhitelistStore is a IPWhitelistStore that uses Redis for persistance
type RedisIPWhitelistStore struct {
	redis  *redis.Client
	cache  lockingCache
	mu     sync.RWMutex
	logger logrus.FieldLogger
}

type lockingCache struct {
	sync.RWMutex
	c []net.IPNet
}

func (rs *RedisIPWhitelistStore) GetWhitelist() ([]net.IPNet, error) {
	rs.cache.RLock()
	defer rs.cache.RUnlock()

	return append([]net.IPNet{}, rs.cache.c...), nil
}

func (rs *RedisIPWhitelistStore) AddCidrs(cidrs []net.IPNet) error {
	key := NamespacedKey(ipWhitelistStoreNamespace, ipWhitelistKey)
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

func (rs *RedisIPWhitelistStore) RemoveCidrs(cidrs []net.IPNet) error {
	key := NamespacedKey(ipWhitelistStoreNamespace, ipWhitelistKey)
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

func (rs *RedisIPWhitelistStore) FetchWhitelist() ([]net.IPNet, error) {
	key := NamespacedKey(ipWhitelistStoreNamespace, ipWhitelistKey)

	rs.logger.Debugf("Sending HKEYS for key %v", key)

	hkeys := rs.redis.HKeys(key)
	if hkeys.Err() != nil {
		return nil, errors.Wrap(hkeys.Err(), "error executing HKEYS")
	}

	whitelistStrings := hkeys.Val()
	whitelist := []net.IPNet{}
	for _, cidrString := range whitelistStrings {
		_, cidr, err := net.ParseCIDR(cidrString)
		if err != nil {
			rs.logger.WithError(err).Errorf("Error parsing cidr from %v", cidrString)
			continue
		}

		whitelist = append(whitelist, *cidr)
	}

	return whitelist, nil
}

func (rs *RedisIPWhitelistStore) RunCacheUpdate(updateInterval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(updateInterval)
	for {
		select {
		case <-ticker.C:
			rs.UpdateCachedWhitelist()
		case <-stop:
			ticker.Stop()
			return
		}
	}
}

func (rs *RedisIPWhitelistStore) UpdateCachedWhitelist() error {
	rs.logger.Debug("Updating whitelist")
	rs.logger.Debug("Fetching whitelist")
	whitelist, err := rs.FetchWhitelist()
	if err != nil {
		rs.logger.WithError(err).Error("Error fetching whitelist")
		return errors.Wrap(err, "error fetching whitelist")
	}
	rs.logger.Debugf("Fetched whitelist with length %d", len(whitelist))

	rs.cache.Lock()
	rs.cache.c = whitelist
	rs.cache.Unlock()
	rs.logger.Debug("Updated whitelist")

	return nil
}
