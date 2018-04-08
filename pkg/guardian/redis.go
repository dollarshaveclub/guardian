package guardian

import (
	"context"
	"fmt"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func NewRedisLimitStore(limit Limit, opts RedisPoolOpts, logger logrus.FieldLogger) *RedisLimitStore {
	pool := &redis.Pool{
		MaxIdle:   opts.MaxIdleConns,
		MaxActive: opts.MaxActiveConns,
		Wait:      opts.Wait,
		Dial:      func() (redis.Conn, error) { return redis.Dial("tcp", opts.Addr) },
	}

	return &RedisLimitStore{limit: limit, pool: pool, logger: logger}
}

// RedisPoolOpts are configuration options used to set up a pool of redis connections
type RedisPoolOpts struct {
	Addr           string
	MaxIdleConns   int
	MaxActiveConns int
	Wait           bool
}

// RedisLimitStore is a LimitStore that uses Redis for persistance
// TODO: fetch the current limit configuration from redis instead of using
// a static one
type RedisLimitStore struct {
	limit  Limit
	pool   *redis.Pool
	logger logrus.FieldLogger
}

func (rs *RedisLimitStore) GetLimit() Limit {
	return rs.limit
}

func (rs *RedisLimitStore) Incr(context context.Context, key string, incrBy uint, expireIn time.Duration) (uint64, error) {
	conn := rs.pool.Get()
	defer conn.Close()

	rs.logger.Debugf("Sending pipeline INCRBY %v %v EXPIRE %v", key, incrBy, expireIn.Seconds())

	// TODO: do we really need transaction gurantees for this?
	conn.Send("MULTI")
	conn.Send("INCRBY", key, incrBy)
	conn.Send("EXPIRE", key, expireIn.Seconds())
	r, err := conn.Do("EXEC")

	if err != nil {
		msg := fmt.Sprintf("error incrementing key %v with increase %d and expiration %v", key, incrBy, expireIn)
		err = errors.Wrap(err, msg)
		rs.logger.WithError(err).Error("error executing pipeline")
		return 0, err
	}

	arr, ok := r.([]interface{})
	if !ok || len(arr) != 2 {
		err = fmt.Errorf("unexpected response from redis: %v", r)
		rs.logger.WithError(err).Error("error executing pipeline")
		return 0, err
	}

	count := arr[0]
	expire := arr[1]

	count64, ok := count.(int64)
	if !ok {
		err = fmt.Errorf("unexpected response for INCRBY: %v", count)
		rs.logger.WithError(err).Error("error executing pipeline")
		return 0, err
	}

	expire64, ok := expire.(int64)
	if !ok {
		err = fmt.Errorf("unexpected response for EXPIRE: %v", expire)
		rs.logger.WithError(err).Error("error executing pipeline")
		return uint64(count64), err
	}

	if expire64 != 1 {
		err = fmt.Errorf("expire timeout not set, expire response: %v", expire64)
		rs.logger.WithError(err).Error("error executing pipeline")
		return uint64(count64), err
	}

	rs.logger.Debugf("Successfully executed pipeline and got response: %v %v", count64, expire64)
	return uint64(count64), nil
}
