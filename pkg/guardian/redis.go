package guardian

import (
	"context"
	"fmt"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/pkg/errors"
)

func NewRedisLimitStore(limit Limit, opts RedisPoolOpts) *RedisLimitStore {
	pool := &redis.Pool{
		MaxIdle:   opts.MaxIdleConns,
		MaxActive: opts.MaxActiveConns,
		Wait:      opts.Wait,
		Dial:      func() (redis.Conn, error) { return redis.Dial("tcp", opts.Addr) },
	}

	return &RedisLimitStore{limit: limit, pool: pool}
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
	limit Limit
	pool  *redis.Pool
}

func (rs *RedisLimitStore) GetLimit() Limit {
	return rs.limit
}

func (rs *RedisLimitStore) Incr(context context.Context, key string, count uint, expireIn time.Duration) (uint64, error) {
	conn := rs.pool.Get()
	defer conn.Close()

	// TODO: do we really need transaction gurantees for this?
	conn.Send("MULTI")
	conn.Send("INCRBY", key, count)
	conn.Send("EXPIRE", key, expireIn*time.Second)
	r, err := conn.Do("EXEC")

	if err != nil {
		msg := fmt.Sprintf("error incrementing key %v with count %d and expiration %v", key, count, expireIn)
		return 0, errors.Wrap(err, msg)
	}

	// the redis client returns ints as int64
	// so we do some sanity checking to ensure
	// we always return >= 0
	i, ok := r.(int64)
	if !ok || i < 0 {
		return 0, fmt.Errorf("unexpect redis from redis: %v", r)
	}

	return uint64(i), nil
}
