package guardian

import (
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/alicebob/miniredis"
	"github.com/go-redis/redis"
	"github.com/google/go-cmp/cmp"
)

func newTestConfStore(t *testing.T) (*RedisConfStore, *miniredis.Miniredis) {
	return newTestConfStoreWithDefaults(t, []net.IPNet{}, []net.IPNet{}, Limit{}, false)
}

func newTestConfStoreWithDefaults(t *testing.T, defaultWhitelist []net.IPNet, defaultBlacklist []net.IPNet, defaultLimit Limit, defaultReportOnly bool) (*RedisConfStore, *miniredis.Miniredis) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("error creating miniredis")
	}

	redis := redis.NewClient(&redis.Options{Addr: s.Addr()})
	rcf, err := NewRedisConfStore(redis, defaultWhitelist, defaultBlacklist, defaultLimit, defaultReportOnly, false, 1000, TestingLogger, NullReporter{})
	if err != nil {
		t.Fatalf("unexpected error creating RedisConfStore: %v", err)
	}
	return rcf, s
}

func TestConfStoreReturnsDefaults(t *testing.T) {
	expectedWhitelist := parseCIDRs([]string{"10.0.0.1/8"})
	expectedBlacklist := parseCIDRs([]string{"12.0.0.1/8"})
	expectedLimit := Limit{Count: 20, Duration: time.Second, Enabled: true}
	expectedReportOnly := true

	c, s := newTestConfStoreWithDefaults(t, expectedWhitelist, expectedBlacklist, expectedLimit, expectedReportOnly)
	defer s.Close()

	gotWhitelist := c.GetWhitelist()
	gotBlacklist := c.GetBlacklist()
	gotLimit := c.GetLimit()
	gotReportOnly := c.GetReportOnly()

	if !cmp.Equal(gotWhitelist, expectedWhitelist) {
		t.Errorf("expected: %v received: %v", expectedWhitelist, gotWhitelist)
	}

	if !cmp.Equal(gotBlacklist, expectedBlacklist) {
		t.Errorf("expected: %v received: %v", expectedWhitelist, gotWhitelist)
	}

	if gotLimit != expectedLimit {
		t.Errorf("expected: %v received: %v", expectedLimit, gotLimit)
	}

	if gotReportOnly != expectedReportOnly {
		t.Errorf("expected: %v received: %v", expectedReportOnly, gotReportOnly)
	}
}

func TestConfStoreReturnsEmptyWhitelistIfNil(t *testing.T) {
	expectedWhitelist := []net.IPNet{}
	expectedLimit := Limit{Count: 20, Duration: time.Second, Enabled: true}
	expectedReportOnly := true

	c, s := newTestConfStoreWithDefaults(t, nil, nil, expectedLimit, expectedReportOnly)
	defer s.Close()

	gotWhitelist := c.GetWhitelist()

	if !cmp.Equal(gotWhitelist, expectedWhitelist) {
		t.Errorf("expected: %v received: %v", expectedWhitelist, gotWhitelist)
	}
}

func TestConfStoreFetchesSets(t *testing.T) {
	c, s := newTestConfStore(t)
	defer s.Close()

	expectedWhitelist := parseCIDRs([]string{"10.0.0.1/8"})
	expectedBlacklist := parseCIDRs([]string{"12.0.0.1/8"})
	expectedLimit := Limit{Count: 20, Duration: time.Second, Enabled: true}
	expectedReportOnly := true
	fooBarURL, _ := url.Parse("/foo/bar")
	expectedRouteRateLimits := map[url.URL]Limit{
		*fooBarURL: Limit{
			Count:    5,
			Duration: time.Second,
			Enabled:  true,
		},
	}
	expectedJails := map[url.URL]Jail{
		*fooBarURL: {
			Limit: Limit{
				Count:    10,
				Duration: time.Minute,
				Enabled:  true,
			},
			BanDuration: time.Hour,
		},
	}

	if err := c.AddWhitelistCidrs(expectedWhitelist); err != nil {
		t.Fatalf("got error: %v", err)
	}

	if err := c.AddBlacklistCidrs(expectedBlacklist); err != nil {
		t.Fatalf("got error: %v", err)
	}

	if err := c.SetLimitDeprecated(expectedLimit); err != nil {
		t.Fatalf("got error: %v", err)
	}

	if err := c.SetReportOnlyDeprecated(expectedReportOnly); err != nil {
		t.Fatalf("got error: %v", err)
	}

	if err := c.SetRouteRateLimitsDeprecated(expectedRouteRateLimits); err != nil {
		t.Fatalf("got error: %v", err)
	}

	if err := c.SetJailsDeprecated(expectedJails); err != nil {
		t.Fatalf("got error: %v", err)
	}

	gotWhitelist, err := c.FetchWhitelist()
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	gotBlacklist, err := c.FetchBlacklist()
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	gotLimit, err := c.FetchLimitDeprecated()
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	gotReportOnly, err := c.FetchReportOnlyDeprecated()
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	gotRouteRateLimits, err := c.FetchRouteRateLimitsDeprecated()
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	gotJails, err := c.FetchJailsDeprecated()
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	if !cmp.Equal(gotWhitelist, expectedWhitelist) {
		t.Errorf("expected: %v received: %v", expectedWhitelist, gotWhitelist)
	}

	if !cmp.Equal(gotBlacklist, expectedBlacklist) {
		t.Errorf("expected: %v received: %v", expectedBlacklist, gotBlacklist)
	}

	if gotLimit != expectedLimit {
		t.Errorf("expected: %v received: %v", expectedLimit, gotLimit)
	}

	if gotReportOnly != expectedReportOnly {
		t.Errorf("expected: %v received: %v", expectedReportOnly, gotReportOnly)
	}

	if !cmp.Equal(gotRouteRateLimits, expectedRouteRateLimits) {
		t.Errorf("expected: %v received: %v", expectedRouteRateLimits, gotRouteRateLimits)
	}

	if !cmp.Equal(gotJails, expectedJails) {
		t.Errorf("expected: %v, received: %v", expectedJails, gotJails)
	}
}

func TestConfStoreUpdateCacheConf(t *testing.T) {
	c, s := newTestConfStore(t)
	defer s.Close()

	expectedWhitelist := parseCIDRs([]string{"10.0.0.1/8"})
	expectedBlacklist := parseCIDRs([]string{"12.0.0.1/8"})
	expectedLimit := Limit{Count: 20, Duration: time.Second, Enabled: true}
	expectedReportOnly := true

	if err := c.AddWhitelistCidrs(expectedWhitelist); err != nil {
		t.Fatalf("got error: %v", err)
	}

	if err := c.AddBlacklistCidrs(expectedBlacklist); err != nil {
		t.Fatalf("got error: %v", err)
	}

	if err := c.SetLimitDeprecated(expectedLimit); err != nil {
		t.Fatalf("got error: %v", err)
	}

	if err := c.SetReportOnlyDeprecated(expectedReportOnly); err != nil {
		t.Fatalf("got error: %v", err)
	}

	c.UpdateCachedConf()

	gotWhitelist := c.GetWhitelist()
	gotBlacklist := c.GetBlacklist()
	gotLimit := c.GetLimit()
	gotReportOnly := c.GetReportOnly()

	if !cmp.Equal(gotWhitelist, expectedWhitelist) {
		t.Errorf("expected: %v received: %v", expectedWhitelist, gotWhitelist)
	}

	if !cmp.Equal(gotBlacklist, expectedBlacklist) {
		t.Errorf("expected: %v received: %v", expectedBlacklist, gotBlacklist)
	}

	if gotLimit != expectedLimit {
		t.Errorf("expected: %v received: %v", expectedLimit, gotLimit)
	}

	if gotReportOnly != expectedReportOnly {
		t.Errorf("expected: %v received: %v", expectedReportOnly, gotReportOnly)
	}
}

func TestConfStoreRunUpdatesCache(t *testing.T) {
	c, s := newTestConfStore(t)
	defer s.Close()

	expectedWhitelist := parseCIDRs([]string{"10.1.1.1/8"})
	expectedBlacklist := parseCIDRs([]string{"11.1.1.1/8"})
	expectedLimit := Limit{Count: 40, Duration: time.Minute, Enabled: true}
	expectedReportOnly := true

	if err := c.AddWhitelistCidrs(expectedWhitelist); err != nil {
		t.Fatalf("got error: %v", err)
	}

	if err := c.AddBlacklistCidrs(expectedBlacklist); err != nil {
		t.Fatalf("got error: %v", err)
	}

	if err := c.SetLimitDeprecated(expectedLimit); err != nil {
		t.Fatalf("got error: %v", err)
	}

	if err := c.SetReportOnlyDeprecated(expectedReportOnly); err != nil {
		t.Fatalf("got error: %v", err)
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		c.RunSync(1*time.Second, stop)
		close(done)
	}()
	time.Sleep(2 * time.Second)
	close(stop)
	<-done

	gotWhitelist := c.GetWhitelist()
	gotBlacklist := c.GetBlacklist()
	gotLimit := c.GetLimit()
	gotReportOnly := c.GetReportOnly()

	if !cmp.Equal(gotWhitelist, expectedWhitelist) {
		t.Errorf("expected: %v received: %v", expectedWhitelist, gotWhitelist)
	}

	if !cmp.Equal(gotBlacklist, expectedBlacklist) {
		t.Errorf("expected: %v received: %v", expectedWhitelist, gotWhitelist)
	}

	if gotLimit != expectedLimit {
		t.Errorf("expected: %v received: %v", expectedLimit, gotLimit)
	}

	if gotReportOnly != expectedReportOnly {
		t.Errorf("expected: %v received: %v", expectedReportOnly, gotReportOnly)
	}
}

func TestConfStoreRemoveWhitelistCidr(t *testing.T) {
	c, s := newTestConfStore(t)
	defer s.Close()

	addWhitelist := parseCIDRs([]string{"10.1.1.1/8", "192.168.1.1/24"})
	if err := c.AddWhitelistCidrs(addWhitelist); err != nil {
		t.Fatalf("got error: %v", err)
	}

	if err := c.RemoveWhitelistCidrs(parseCIDRs([]string{"10.1.1.1/8"})); err != nil {
		t.Fatalf("got error: %v", err)
	}

	gotWhitelist, err := c.FetchWhitelist()
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	expectedWhitelist := parseCIDRs([]string{"192.168.1.1/24"})
	if !cmp.Equal(gotWhitelist, expectedWhitelist) {
		t.Errorf("expected: %v received: %v", expectedWhitelist, gotWhitelist)
	}
}

func TestConfStoreRemoveBlacklistCidr(t *testing.T) {
	c, s := newTestConfStore(t)
	defer s.Close()

	addBlacklist := parseCIDRs([]string{"10.1.1.1/8", "192.168.1.1/24"})
	if err := c.AddBlacklistCidrs(addBlacklist); err != nil {
		t.Fatalf("got error: %v", err)
	}

	if err := c.RemoveBlacklistCidrs(parseCIDRs([]string{"10.1.1.1/8"})); err != nil {
		t.Fatalf("got error: %v", err)
	}

	got, err := c.FetchBlacklist()
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	expected := parseCIDRs([]string{"192.168.1.1/24"})
	if !cmp.Equal(got, expected) {
		t.Errorf("expected: %v received: %v", expected, got)
	}
}

func TestConfStoreAddRemoveRouteRateLimits(t *testing.T) {
	c, s := newTestConfStore(t)
	defer s.Close()
	fooBarURL, _ := url.Parse("/foo/bar")
	fooBarLimit := Limit{
		Count:    5,
		Duration: time.Second,
		Enabled:  true,
	}

	fooBazURL, _ := url.Parse("/foo/baz")
	fooBazLimit := Limit{
		Count:    3,
		Duration: time.Second,
		Enabled:  false,
	}

	routeRateLimits := map[url.URL]Limit{
		*fooBarURL: fooBarLimit,
		*fooBazURL: fooBazLimit,
	}

	if err := c.SetRouteRateLimitsDeprecated(routeRateLimits); err != nil {
		t.Fatalf("got error: %v", err)
	}

	got, err := c.FetchRouteRateLimitDeprecated(*fooBarURL)
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	if !cmp.Equal(got, fooBarLimit) {
		t.Errorf("expected: %v, received: %v", fooBarLimit, got)
	}

	// Ensure configuration cache is updated after a confSyncInterval
	c.UpdateCachedConf()
	cachedItem := c.GetRouteRateLimit(*fooBarURL)
	if !cmp.Equal(cachedItem, fooBarLimit) {
		t.Errorf("expected: %v, received: %v", fooBarLimit, cachedItem)
	}

	var urls []url.URL
	urls = append(urls, *fooBarURL)
	if err := c.RemoveRouteRateLimitsDeprecated(urls); err != nil {
		t.Fatalf("got error: %v", err)
	}

	// Expect an error since we removed the limits for this route
	got, err = c.FetchRouteRateLimitDeprecated(*fooBarURL)
	if err == nil {
		t.Fatalf("expected error fetching route limit which didn't exist")
	}

	// Ensure configuration cache is updated after a confSyncInterval
	c.UpdateCachedConf()
	cachedItem = c.GetRouteRateLimit(*fooBarURL)
	if !cmp.Equal(cachedItem, Limit{}) {
		t.Errorf("expected: %v, received: %v", Limit{}, cachedItem)
	}

	got, err = c.FetchRouteRateLimitDeprecated(*fooBazURL)
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	if !cmp.Equal(got, fooBazLimit) {
		t.Errorf("expected: %v, received: %v", fooBazLimit, got)
	}
}

func TestConfStoreSetExistingRoute(t *testing.T) {
	c, s := newTestConfStore(t)
	defer s.Close()
	fooBarURL, _ := url.Parse("/foo/bar")
	originalRouteRateLimit := map[url.URL]Limit{
		*fooBarURL: Limit{
			Count:    5,
			Duration: time.Second,
			Enabled:  true,
		},
	}

	if err := c.SetRouteRateLimitsDeprecated(originalRouteRateLimit); err != nil {
		t.Fatalf("got error: %v", err)
	}

	newLimit := Limit{
		Count:    5,
		Duration: time.Second,
		Enabled:  true,
	}

	newRouteRateLimit := map[url.URL]Limit{
		*fooBarURL: newLimit,
	}
	if err := c.SetRouteRateLimitsDeprecated(newRouteRateLimit); err != nil {
		t.Fatalf("got error: %v", err)
	}

	got, err := c.FetchRouteRateLimitDeprecated(*fooBarURL)
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	if !cmp.Equal(got, newLimit) {
		t.Errorf("expected: %v, received: %v", newLimit, got)
	}
}

func TestConfStoreRemoveNonexistentRoute(t *testing.T) {
	c, s := newTestConfStore(t)
	defer s.Close()
	fooBarURL, _ := url.Parse("/foo/bar")
	fooBarLimit := Limit{
		Count:    5,
		Duration: time.Second,
		Enabled:  true,
	}

	fooBazURL, _ := url.Parse("/foo/baz")
	fooBazLimit := Limit{
		Count:    3,
		Duration: time.Second,
		Enabled:  false,
	}

	routeRateLimits := map[url.URL]Limit{
		*fooBarURL: fooBarLimit,
		*fooBazURL: fooBazLimit,
	}

	if err := c.SetRouteRateLimitsDeprecated(routeRateLimits); err != nil {
		t.Fatalf("got error: %v", err)
	}

	var urls []url.URL
	nonExistentURL, _ := url.Parse("/foo/foo")
	urls = append(urls, *nonExistentURL, *fooBarURL)
	if err := c.RemoveRouteRateLimitsDeprecated(urls); err != nil {
		t.Fatalf("got error: %v", err)
	}

	// Expect an error since we removed the limits for this route
	got, err := c.FetchRouteRateLimitDeprecated(*fooBarURL)
	if err == nil {
		t.Fatalf("expected error fetching route limit which didn't exist")
	}

	got, err = c.FetchRouteRateLimitDeprecated(*fooBazURL)
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	if !cmp.Equal(got, fooBazLimit) {
		t.Errorf("expected: %v, received: %v", fooBazLimit, got)
	}
}

func TestConfStoreAddRemoveJails(t *testing.T) {
	c, s := newTestConfStore(t)
	defer s.Close()

	fooBarURL, _ := url.Parse("/foo/bar")
	fooBarJail := Jail{
		Limit: Limit{
			Count:    5,
			Duration: time.Second,
			Enabled:  true,
		},
		BanDuration: time.Hour,
	}

	fooBazURL, _ := url.Parse("/foo/baz")
	fooBazJail := Jail{
		Limit: Limit{
			Count:    3,
			Duration: time.Second,
			Enabled:  false,
		},
		BanDuration: time.Hour,
	}

	jails := map[url.URL]Jail{
		*fooBarURL: fooBarJail,
		*fooBazURL: fooBazJail,
	}

	if err := c.SetJailsDeprecated(jails); err != nil {
		t.Fatalf("got error: %v", err)
	}

	got, err := c.FetchJailDeprecated(*fooBarURL)
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	if !cmp.Equal(got, fooBarJail) {
		t.Errorf("expected: %v, received: %v", fooBarJail, got)
	}

	// Ensure configuration cache is updated after a confSyncInterval
	c.UpdateCachedConf()
	cachedItem := c.GetJail(*fooBarURL)
	if !cmp.Equal(cachedItem, fooBarJail) {
		t.Errorf("expected: %v, received: %v", fooBarJail, cachedItem)
	}

	var urls []url.URL
	urls = append(urls, *fooBarURL)
	if err := c.RemoveJailsDeprecated(urls); err != nil {
		t.Fatalf("got error: %v", err)
	}

	// Expect an error since we removed the limits for this route
	got, err = c.FetchJailDeprecated(*fooBarURL)
	if err == nil {
		t.Fatalf("expected error fetching route limit which didn't exist")
	}

	// Ensure configuration cache is updated after a confSyncInterval
	c.UpdateCachedConf()
	cachedItem = c.GetJail(*fooBarURL)
	if !cmp.Equal(cachedItem, Jail{}) {
		t.Errorf("expected: %v, received: %v", Jail{}, cachedItem)
	}

	got, err = c.FetchJailDeprecated(*fooBazURL)
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	if !cmp.Equal(got, fooBazJail) {
		t.Errorf("expected: %v, received: %v", fooBarJail, got)
	}
}

func TestConfStoreSetExistingJail(t *testing.T) {
	c, s := newTestConfStore(t)
	defer s.Close()

	fooBarURL, _ := url.Parse("/foo/bar")
	fooBarJail := Jail{
		Limit: Limit{
			Count:    5,
			Duration: time.Second,
			Enabled:  true,
		},
		BanDuration: time.Hour,
	}

	jails := map[url.URL]Jail{*fooBarURL: fooBarJail}

	if err := c.SetJailsDeprecated(jails); err != nil {
		t.Fatalf("got error: %v", err)
	}

	newJail := Jail{
		Limit: Limit{
			Count:    100,
			Duration: time.Minute,
			Enabled:  false,
		},
		BanDuration: time.Minute,
	}

	newJails := map[url.URL]Jail{
		*fooBarURL: newJail,
	}
	if err := c.SetJailsDeprecated(newJails); err != nil {
		t.Fatalf("got error: %v", err)
	}

	got, err := c.FetchJailDeprecated(*fooBarURL)
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	if !cmp.Equal(got, newJail) {
		t.Errorf("expected: %v, received: %v", newJail, got)
	}
}

func TestConfStoreAddRemovePrisoners(t *testing.T) {
	c, s := newTestConfStore(t)
	defer s.Close()

	expiredPrisoner := "1.1.1.1"
	expiredJail := Jail{
		Limit: Limit{
			Count:    10,
			Duration: time.Minute,
			Enabled:  true,
		},
		BanDuration: 0 * time.Millisecond,
	}
	currentPrisoner := "2.2.2.2"
	currentJail := Jail{
		Limit: Limit{
			Count:    10,
			Duration: time.Minute,
			Enabled:  true,
		},
		BanDuration: 24 * time.Hour,
	}

	c.AddPrisoner(expiredPrisoner, expiredJail)
	c.AddPrisoner(currentPrisoner, currentJail)

	time.Sleep(100 * time.Millisecond)

	in := func(ip string, prisoners []Prisoner) bool {
		for _, p := range prisoners {
			if p.IP.String() == ip {
				return true
			}
		}
		return false
	}
	prisoners, err := c.FetchPrisoners()
	if err != nil {
		t.Errorf("unexpected error fetching prisoners: %v", err)
	}

	if in(expiredPrisoner, prisoners) {
		t.Errorf("expected expired prisoner to be removed: %v", expiredPrisoner)
	}

	if !in(currentPrisoner, prisoners) {
		t.Errorf("expected prisoner: %v", currentPrisoner)
	}

	n, err := c.RemovePrisoners([]net.IP{net.ParseIP(currentPrisoner)})
	if err != nil {
		t.Errorf("received unexpected error when removing prisoner: %v", err)
	}
	if n != 1 {
		t.Errorf("expected %d prisoner(s) removed, received %d", 1, n)
	}
	prisoners, err = c.FetchPrisoners()
	if err != nil {
		t.Errorf("unexpected error fetching prisoners: %v", err)
	}

	if in(currentPrisoner, prisoners) {
		t.Errorf("expected prisoner %v to be removed", currentPrisoner)
	}
}
