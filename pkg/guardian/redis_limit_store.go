package guardian

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const limitStoreNamespace = "limit_store"

func NewRedisLimitStore(limit Limit, redis *redis.Client, logger logrus.FieldLogger) *RedisLimitStore {
	return &RedisLimitStore{limit: limit, redis: redis, logger: logger}
}

// RedisLimitStore is a LimitStore that uses Redis for persistance
// TODO: fetch the current limit configuration from redis instead of using
// a static one
type RedisLimitStore struct {
	limit  Limit
	redis  *redis.Client
	logger logrus.FieldLogger
}

func (rs *RedisLimitStore) GetLimit() Limit {
	return rs.limit
}

func (rs *RedisLimitStore) Incr(context context.Context, key string, incrBy uint, expireIn time.Duration) (uint64, error) {
	key = NamespacedKey(limitStoreNamespace, key)

	rs.logger.Debugf("Sending pipeline INCRBY %v %v EXPIRE %v", key, incrBy, expireIn.Seconds())

	pipe := rs.redis.Pipeline()
	incr := pipe.IncrBy(key, int64(incrBy))
	expire := pipe.Expire(key, expireIn)
	_, err := pipe.Exec()
	if err != nil {
		msg := fmt.Sprintf("error incrementing key %v with increase %d and expiration %v", key, incrBy, expireIn)
		err = errors.Wrap(err, msg)
		rs.logger.WithError(err).Error("error executing pipeline")
		return 0, err
	}

	count := uint64(incr.Val())
	expireSet := expire.Val()

	if expireSet == false {
		err = fmt.Errorf("expire timeout not set, key does not exist")
		rs.logger.WithError(err).Error("error executing pipeline")
		return count, err
	}

	rs.logger.Debugf("Successfully executed pipeline and got response: %v %v", count, expireSet)
	return count, nil
}
