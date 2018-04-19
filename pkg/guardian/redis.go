package guardian

import (
	"fmt"
	"time"
)

// DefaultRedisDialTimeout is the default timeout used when connecting to redis
var DefaultRedisDialTimeout = 100 * time.Millisecond

// DefaultRedisReadTimeout is the default timeout used when reading a reply from redis
var DefaultRedisReadTimeout = 100 * time.Millisecond

// DefaultRedisWriteTimeout is the default timeout used when writing to redis
var DefaultRedisWriteTimeout = 100 * time.Millisecond

// NamespacedKey returns a key with the namespace prepended
func NamespacedKey(namespace, key string) string {
	return fmt.Sprintf("%v:%v", namespace, key)
}
