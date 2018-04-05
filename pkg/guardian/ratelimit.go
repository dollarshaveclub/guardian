package guardian

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/pkg/errors"
)

// Limit describes a rate limit
type Limit struct {
	Count    uint64
	Duration time.Duration
}

// LimitStore is a data store capable of storing, incrementing and expiring the count of a key
type LimitStore interface {
	// GetLimit returns the current limit settings
	GetLimit() Limit

	// Incr increments key by count and sets the expiration to expireIn from now. The result or an error is returned
	// uint64 is used to accomodate the largest count possible
	Incr(context context.Context, key string, count uint, expireIn time.Duration) (uint64, error)
}

// NewIPRateLimiter creates a new IP rate limiter using the given store
func NewIPRateLimiter(store LimitStore) (*IPRateLimiter, error) {
	if store == nil {
		return nil, fmt.Errorf("invalid store")
	}

	return &IPRateLimiter{store: store}, nil
}

// IPRateLimiter is an IP based rate limiter
type IPRateLimiter struct {
	store LimitStore
}

// Limit limits a request if request exceeds rate limit
func (rl *IPRateLimiter) Limit(context context.Context, request Request) (bool, uint32, error) {

	limit := rl.store.GetLimit()
	key := rl.SlotKey(request, time.Now(), limit)

	currCount, err := rl.store.Incr(context, key, 1, limit.Duration)
	if err != nil {
		return false, 0, errors.Wrap(err, fmt.Sprintf("error incrementing limit for request %v", request))
	}

	if currCount > limit.Count {
		return true, 0, nil // block request, rate limited
	}

	remaining64 := limit.Count - currCount
	remaining32 := uint32(remaining64)
	if uint64(remaining32) != remaining64 { // if we lose some signifcant bits, convert it to max of uint32
		remaining32 = ^uint32(0)
	}

	return false, remaining32, nil
}

// SlotKey generates the key for a slot determined by the request, slot time, and limit duration
func (rl *IPRateLimiter) SlotKey(request Request, slotTime time.Time, limit Limit) string {
	// a) convert to seconds
	// b) get slot time unix epoch seconds
	// c) use integer division to bucket based on limit.Duration
	// if secs = 10
	// 1522895020 -> 1522895020
	// 1522895021 -> 1522895020
	// 1522895028 -> 1522895020
	// 1522895030 -> 1522895030
	secs := int64(limit.Duration / time.Second) // a
	t := slotTime.Unix()                        // b
	slot := (t / secs) * secs                   // c
	key := request.RemoteAddress + ":" + strconv.FormatInt(slot, 10)
	return key
}
