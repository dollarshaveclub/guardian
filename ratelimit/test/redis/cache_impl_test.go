package redis_test

import (
	"testing"

	pb "github.com/dollarshaveclub/guardian/ratelimit/proto/ratelimit"
	"github.com/dollarshaveclub/guardian/ratelimit/src/config"
	"github.com/dollarshaveclub/guardian/ratelimit/src/redis"
	"github.com/lyft/gostats"

	"math/rand"

	"github.com/dollarshaveclub/guardian/ratelimit/test/common"
	"github.com/dollarshaveclub/guardian/ratelimit/test/mocks/redis"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestRedis(t *testing.T) {
	assert := assert.New(t)
	controller := gomock.NewController(t)
	defer controller.Finish()

	pool := mock_redis.NewMockPool(controller)
	timeSource := mock_redis.NewMockTimeSource(controller)
	connection := mock_redis.NewMockConnection(controller)
	response := mock_redis.NewMockResponse(controller)
	cache := redis.NewRateLimitCacheImpl(pool, timeSource, rand.New(rand.NewSource(1)), 0)
	statsStore := stats.NewStore(stats.NewNullSink(), false)

	pool.EXPECT().Get().Return(connection)
	timeSource.EXPECT().UnixNow().Return(int64(1234))
	connection.EXPECT().PipeAppend("INCRBY", "domain_key_value_1234", uint32(1))
	connection.EXPECT().PipeAppend("EXPIRE", "domain_key_value_1234", int64(1))
	connection.EXPECT().PipeResponse().Return(response)
	response.EXPECT().Int().Return(int64(5))
	connection.EXPECT().PipeResponse()
	pool.EXPECT().Put(connection)

	request := common.NewRateLimitRequest("domain", [][][2]string{{{"key", "value"}}}, 1)
	limits := []*config.RateLimit{config.NewRateLimit(10, pb.RateLimit_SECOND, "key_value", statsStore)}

	assert.Equal(
		[]*pb.RateLimitResponse_DescriptorStatus{{pb.RateLimitResponse_OK, limits[0].Limit, 5}},
		cache.DoLimit(nil, request, limits))
	assert.Equal(uint64(1), limits[0].Stats.TotalHits.Value())
	assert.Equal(uint64(0), limits[0].Stats.OverLimit.Value())
	assert.Equal(uint64(0), limits[0].Stats.NearLimit.Value())

	pool.EXPECT().Get().Return(connection)
	timeSource.EXPECT().UnixNow().Return(int64(1234))
	connection.EXPECT().PipeAppend("INCRBY", "domain_key2_value2_subkey2_subvalue2_1200", uint32(1))
	connection.EXPECT().PipeAppend(
		"EXPIRE", "domain_key2_value2_subkey2_subvalue2_1200", int64(60))
	connection.EXPECT().PipeResponse().Return(response)
	response.EXPECT().Int().Return(int64(11))
	connection.EXPECT().PipeResponse()
	pool.EXPECT().Put(connection)

	request = common.NewRateLimitRequest(
		"domain",
		[][][2]string{
			{{"key2", "value2"}},
			{{"key2", "value2"}, {"subkey2", "subvalue2"}},
		}, 1)
	limits = []*config.RateLimit{
		nil,
		config.NewRateLimit(10, pb.RateLimit_MINUTE, "key2_value2_subkey2_subvalue2", statsStore)}
	assert.Equal(
		[]*pb.RateLimitResponse_DescriptorStatus{{pb.RateLimitResponse_OK, nil, 0},
			{pb.RateLimitResponse_OVER_LIMIT, limits[1].Limit, 0}},
		cache.DoLimit(nil, request, limits))
	assert.Equal(uint64(1), limits[1].Stats.TotalHits.Value())
	assert.Equal(uint64(1), limits[1].Stats.OverLimit.Value())
	assert.Equal(uint64(0), limits[1].Stats.NearLimit.Value())

	pool.EXPECT().Get().Return(connection)
	timeSource.EXPECT().UnixNow().Return(int64(1000000))
	connection.EXPECT().PipeAppend("INCRBY", "domain_key3_value3_997200", uint32(1))
	connection.EXPECT().PipeAppend(
		"EXPIRE", "domain_key3_value3_997200", int64(3600))
	connection.EXPECT().PipeAppend("INCRBY", "domain_key3_value3_subkey3_subvalue3_950400", uint32(1))
	connection.EXPECT().PipeAppend(
		"EXPIRE", "domain_key3_value3_subkey3_subvalue3_950400", int64(86400))
	connection.EXPECT().PipeResponse().Return(response)
	response.EXPECT().Int().Return(int64(11))
	connection.EXPECT().PipeResponse()
	connection.EXPECT().PipeResponse().Return(response)
	response.EXPECT().Int().Return(int64(13))
	connection.EXPECT().PipeResponse()
	pool.EXPECT().Put(connection)

	request = common.NewRateLimitRequest(
		"domain",
		[][][2]string{
			{{"key3", "value3"}},
			{{"key3", "value3"}, {"subkey3", "subvalue3"}},
		}, 1)
	limits = []*config.RateLimit{
		config.NewRateLimit(10, pb.RateLimit_HOUR, "key3_value3", statsStore),
		config.NewRateLimit(10, pb.RateLimit_DAY, "key3_value3_subkey3_subvalue3", statsStore)}
	assert.Equal(
		[]*pb.RateLimitResponse_DescriptorStatus{
			{pb.RateLimitResponse_OVER_LIMIT, limits[0].Limit, 0},
			{pb.RateLimitResponse_OVER_LIMIT, limits[1].Limit, 0}},
		cache.DoLimit(nil, request, limits))
	assert.Equal(uint64(1), limits[0].Stats.TotalHits.Value())
	assert.Equal(uint64(1), limits[0].Stats.OverLimit.Value())
	assert.Equal(uint64(0), limits[0].Stats.NearLimit.Value())
	assert.Equal(uint64(1), limits[0].Stats.TotalHits.Value())
	assert.Equal(uint64(1), limits[0].Stats.OverLimit.Value())
	assert.Equal(uint64(0), limits[0].Stats.NearLimit.Value())

	// Test Near Limit Stats. Under Near Limit Ratio
	pool.EXPECT().Get().Return(connection)
	timeSource.EXPECT().UnixNow().Return(int64(1000000))
	connection.EXPECT().PipeAppend("INCRBY", "domain_key4_value4_997200", uint32(1))
	connection.EXPECT().PipeAppend(
		"EXPIRE", "domain_key4_value4_997200", int64(3600))
	connection.EXPECT().PipeResponse().Return(response)
	response.EXPECT().Int().Return(int64(11))
	connection.EXPECT().PipeResponse()
	pool.EXPECT().Put(connection)

	request = common.NewRateLimitRequest("domain", [][][2]string{{{"key4", "value4"}}}, 1)

	limits = []*config.RateLimit{
		config.NewRateLimit(15, pb.RateLimit_HOUR, "key4_value4", statsStore)}

	assert.Equal(
		[]*pb.RateLimitResponse_DescriptorStatus{
			{pb.RateLimitResponse_OK, limits[0].Limit, 4}},
		cache.DoLimit(nil, request, limits))
	assert.Equal(uint64(1), limits[0].Stats.TotalHits.Value())
	assert.Equal(uint64(0), limits[0].Stats.OverLimit.Value())
	assert.Equal(uint64(0), limits[0].Stats.NearLimit.Value())

	// Test Near Limit Stats. At Near Limit Ratio, still OK
	pool.EXPECT().Get().Return(connection)
	timeSource.EXPECT().UnixNow().Return(int64(1000000))
	connection.EXPECT().PipeAppend("INCRBY", "domain_key4_value4_997200", uint32(1))
	connection.EXPECT().PipeAppend(
		"EXPIRE", "domain_key4_value4_997200", int64(3600))
	connection.EXPECT().PipeResponse().Return(response)
	response.EXPECT().Int().Return(int64(13))
	connection.EXPECT().PipeResponse()
	pool.EXPECT().Put(connection)

	assert.Equal(
		[]*pb.RateLimitResponse_DescriptorStatus{
			{pb.RateLimitResponse_OK, limits[0].Limit, 2}},
		cache.DoLimit(nil, request, limits))
	assert.Equal(uint64(2), limits[0].Stats.TotalHits.Value())
	assert.Equal(uint64(0), limits[0].Stats.OverLimit.Value())
	assert.Equal(uint64(1), limits[0].Stats.NearLimit.Value())

	// Test Near Limit Stats. We went OVER_LIMIT, but the near_limit counter only increases
	// when we are near limit, not after we have passed the limit.
	pool.EXPECT().Get().Return(connection)
	timeSource.EXPECT().UnixNow().Return(int64(1000000))
	connection.EXPECT().PipeAppend("INCRBY", "domain_key4_value4_997200", uint32(1))
	connection.EXPECT().PipeAppend(
		"EXPIRE", "domain_key4_value4_997200", int64(3600))
	connection.EXPECT().PipeResponse().Return(response)
	response.EXPECT().Int().Return(int64(16))
	connection.EXPECT().PipeResponse()
	pool.EXPECT().Put(connection)

	assert.Equal(
		[]*pb.RateLimitResponse_DescriptorStatus{
			{pb.RateLimitResponse_OVER_LIMIT, limits[0].Limit, 0}},
		cache.DoLimit(nil, request, limits))
	assert.Equal(uint64(3), limits[0].Stats.TotalHits.Value())
	assert.Equal(uint64(1), limits[0].Stats.OverLimit.Value())
	assert.Equal(uint64(1), limits[0].Stats.NearLimit.Value())

	// Now test hitsAddend that is greater than 1
	// All of it under limit, under near limit
	pool.EXPECT().Get().Return(connection)
	timeSource.EXPECT().UnixNow().Return(int64(1234))
	connection.EXPECT().PipeAppend("INCRBY", "domain_key5_value5_1234", uint32(3))
	connection.EXPECT().PipeAppend("EXPIRE", "domain_key5_value5_1234", int64(1))
	connection.EXPECT().PipeResponse().Return(response)
	response.EXPECT().Int().Return(int64(5))
	connection.EXPECT().PipeResponse()
	pool.EXPECT().Put(connection)

	request = common.NewRateLimitRequest("domain", [][][2]string{{{"key5", "value5"}}}, 3)
	limits = []*config.RateLimit{config.NewRateLimit(20, pb.RateLimit_SECOND, "key5_value5", statsStore)}

	assert.Equal(
		[]*pb.RateLimitResponse_DescriptorStatus{{pb.RateLimitResponse_OK, limits[0].Limit, 15}},
		cache.DoLimit(nil, request, limits))
	assert.Equal(uint64(3), limits[0].Stats.TotalHits.Value())
	assert.Equal(uint64(0), limits[0].Stats.OverLimit.Value())
	assert.Equal(uint64(0), limits[0].Stats.NearLimit.Value())

	// All of it under limit, some over near limit
	pool.EXPECT().Get().Return(connection)
	timeSource.EXPECT().UnixNow().Return(int64(1234))
	connection.EXPECT().PipeAppend("INCRBY", "domain_key6_value6_1234", uint32(2))
	connection.EXPECT().PipeAppend("EXPIRE", "domain_key6_value6_1234", int64(1))
	connection.EXPECT().PipeResponse().Return(response)
	response.EXPECT().Int().Return(int64(7))
	connection.EXPECT().PipeResponse()
	pool.EXPECT().Put(connection)

	request = common.NewRateLimitRequest("domain", [][][2]string{{{"key6", "value6"}}}, 2)
	limits = []*config.RateLimit{config.NewRateLimit(8, pb.RateLimit_SECOND, "key6_value6", statsStore)}

	assert.Equal(
		[]*pb.RateLimitResponse_DescriptorStatus{{pb.RateLimitResponse_OK, limits[0].Limit, 1}},
		cache.DoLimit(nil, request, limits))
	assert.Equal(uint64(2), limits[0].Stats.TotalHits.Value())
	assert.Equal(uint64(0), limits[0].Stats.OverLimit.Value())
	assert.Equal(uint64(1), limits[0].Stats.NearLimit.Value())

	// All of it under limit, all of it over near limit
	pool.EXPECT().Get().Return(connection)
	timeSource.EXPECT().UnixNow().Return(int64(1234))
	connection.EXPECT().PipeAppend("INCRBY", "domain_key7_value7_1234", uint32(3))
	connection.EXPECT().PipeAppend("EXPIRE", "domain_key7_value7_1234", int64(1))
	connection.EXPECT().PipeResponse().Return(response)
	response.EXPECT().Int().Return(int64(19))
	connection.EXPECT().PipeResponse()
	pool.EXPECT().Put(connection)

	request = common.NewRateLimitRequest("domain", [][][2]string{{{"key7", "value7"}}}, 3)
	limits = []*config.RateLimit{config.NewRateLimit(20, pb.RateLimit_SECOND, "key7_value7", statsStore)}

	assert.Equal(
		[]*pb.RateLimitResponse_DescriptorStatus{{pb.RateLimitResponse_OK, limits[0].Limit, 1}},
		cache.DoLimit(nil, request, limits))
	assert.Equal(uint64(3), limits[0].Stats.TotalHits.Value())
	assert.Equal(uint64(0), limits[0].Stats.OverLimit.Value())
	assert.Equal(uint64(3), limits[0].Stats.NearLimit.Value())

	// Some of it over limit, all of it over near limit
	pool.EXPECT().Get().Return(connection)
	timeSource.EXPECT().UnixNow().Return(int64(1234))
	connection.EXPECT().PipeAppend("INCRBY", "domain_key8_value8_1234", uint32(3))
	connection.EXPECT().PipeAppend("EXPIRE", "domain_key8_value8_1234", int64(1))
	connection.EXPECT().PipeResponse().Return(response)
	response.EXPECT().Int().Return(int64(22))
	connection.EXPECT().PipeResponse()
	pool.EXPECT().Put(connection)

	request = common.NewRateLimitRequest("domain", [][][2]string{{{"key8", "value8"}}}, 3)
	limits = []*config.RateLimit{config.NewRateLimit(20, pb.RateLimit_SECOND, "key8_value8", statsStore)}

	assert.Equal(
		[]*pb.RateLimitResponse_DescriptorStatus{{pb.RateLimitResponse_OVER_LIMIT, limits[0].Limit, 0}},
		cache.DoLimit(nil, request, limits))
	assert.Equal(uint64(3), limits[0].Stats.TotalHits.Value())
	assert.Equal(uint64(2), limits[0].Stats.OverLimit.Value())
	assert.Equal(uint64(1), limits[0].Stats.NearLimit.Value())

	// Some of it in all three places
	pool.EXPECT().Get().Return(connection)
	timeSource.EXPECT().UnixNow().Return(int64(1234))
	connection.EXPECT().PipeAppend("INCRBY", "domain_key9_value9_1234", uint32(7))
	connection.EXPECT().PipeAppend("EXPIRE", "domain_key9_value9_1234", int64(1))
	connection.EXPECT().PipeResponse().Return(response)
	response.EXPECT().Int().Return(int64(22))
	connection.EXPECT().PipeResponse()
	pool.EXPECT().Put(connection)

	request = common.NewRateLimitRequest("domain", [][][2]string{{{"key9", "value9"}}}, 7)
	limits = []*config.RateLimit{config.NewRateLimit(20, pb.RateLimit_SECOND, "key9_value9", statsStore)}

	assert.Equal(
		[]*pb.RateLimitResponse_DescriptorStatus{{pb.RateLimitResponse_OVER_LIMIT, limits[0].Limit, 0}},
		cache.DoLimit(nil, request, limits))
	assert.Equal(uint64(7), limits[0].Stats.TotalHits.Value())
	assert.Equal(uint64(2), limits[0].Stats.OverLimit.Value())
	assert.Equal(uint64(4), limits[0].Stats.NearLimit.Value())

	// all of it over limit
	pool.EXPECT().Get().Return(connection)
	timeSource.EXPECT().UnixNow().Return(int64(1234))
	connection.EXPECT().PipeAppend("INCRBY", "domain_key10_value10_1234", uint32(3))
	connection.EXPECT().PipeAppend("EXPIRE", "domain_key10_value10_1234", int64(1))
	connection.EXPECT().PipeResponse().Return(response)
	response.EXPECT().Int().Return(int64(30))
	connection.EXPECT().PipeResponse()
	pool.EXPECT().Put(connection)

	request = common.NewRateLimitRequest("domain", [][][2]string{{{"key10", "value10"}}}, 3)
	limits = []*config.RateLimit{config.NewRateLimit(10, pb.RateLimit_SECOND, "key10_value10", statsStore)}

	assert.Equal(
		[]*pb.RateLimitResponse_DescriptorStatus{{pb.RateLimitResponse_OVER_LIMIT, limits[0].Limit, 0}},
		cache.DoLimit(nil, request, limits))
	assert.Equal(uint64(3), limits[0].Stats.TotalHits.Value())
	assert.Equal(uint64(3), limits[0].Stats.OverLimit.Value())
	assert.Equal(uint64(0), limits[0].Stats.NearLimit.Value())
}

func TestRedisWithJitter(t *testing.T) {
	assert := assert.New(t)
	controller := gomock.NewController(t)
	defer controller.Finish()

	pool := mock_redis.NewMockPool(controller)
	timeSource := mock_redis.NewMockTimeSource(controller)
	connection := mock_redis.NewMockConnection(controller)
	response := mock_redis.NewMockResponse(controller)
	jitterSource := mock_redis.NewMockJitterRandSource(controller)
	cache := redis.NewRateLimitCacheImpl(pool, timeSource, rand.New(jitterSource), 3600)
	statsStore := stats.NewStore(stats.NewNullSink(), false)

	pool.EXPECT().Get().Return(connection)
	timeSource.EXPECT().UnixNow().Return(int64(1234))
	jitterSource.EXPECT().Int63().Return(int64(100))
	connection.EXPECT().PipeAppend("INCRBY", "domain_key_value_1234", uint32(1))
	connection.EXPECT().PipeAppend("EXPIRE", "domain_key_value_1234", int64(101))
	connection.EXPECT().PipeResponse().Return(response)
	response.EXPECT().Int().Return(int64(5))
	connection.EXPECT().PipeResponse()
	pool.EXPECT().Put(connection)

	request := common.NewRateLimitRequest("domain", [][][2]string{{{"key", "value"}}}, 1)
	limits := []*config.RateLimit{config.NewRateLimit(10, pb.RateLimit_SECOND, "key_value", statsStore)}

	assert.Equal(
		[]*pb.RateLimitResponse_DescriptorStatus{{pb.RateLimitResponse_OK, limits[0].Limit, 5}},
		cache.DoLimit(nil, request, limits))
	assert.Equal(uint64(1), limits[0].Stats.TotalHits.Value())
	assert.Equal(uint64(0), limits[0].Stats.OverLimit.Value())
	assert.Equal(uint64(0), limits[0].Stats.NearLimit.Value())
}
