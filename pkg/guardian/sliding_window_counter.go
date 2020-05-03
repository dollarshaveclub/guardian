package guardian

//
//import (
//	"github.com/go-redis/redis"
//	"github.com/sirupsen/logrus"
//	"sync"
//	"time"
//)
//
//const slidingWindowNamespace = "sliding_window"
//
//type bucket struct {
//	val uint64
//	expireAt time.Time
//}
//
//type slidingWindowCache struct {
//	mutex sync.RWMutex
//	m map[string]bucket
//}
//
//type slidingWindowCounter struct {
//	redis *redis.Client
//	synchronous bool
//	logger logrus.FieldLogger
//	reporter MetricReporter
//	cache slidingWindowCache
//}
//
//func (swc *slidingWindowCounter) Run(pruneInterval time.Duration, stop <-chan struct{}) {
//	ticker := time.NewTicker(pruneInterval)
//	for {
//		select {
//		case <-ticker.C:
//			swc.pruneCache(time.Now().UTC())
//		case <-stop:
//			ticker.Stop()
//			return
//		}
//	}
//}
//
//func (swc *slidingWindowCounter) pruneCache(olderThan time.Time) {
//	start := time.Now().UTC()
//	cacheSize := 0
//	pruned := 0
//	defer func() {
//		rs.reporter.RedisCounterPruned(time.Since(start), float64(cacheSize), float64(pruned))
//	}()
//
//	rs.cache.Lock()
//	defer rs.cache.Unlock()
//
//	cacheSize = len(rs.cache.m)
//	for k, v := range rs.cache.m {
//		if v.expireAt.Before(olderThan) {
//			delete(rs.cache.m, k)
//			pruned++
//		}
//	}
//}
