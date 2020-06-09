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
	expectedGlobalRateLimit := GlobalRateLimitConfig{
		ConfigMetadata: ConfigMetadata{
			Version: GlobalRateLimitConfigVersion,
			Kind:    GlobalRateLimitConfigKind,
			Name:    GlobalRateLimitConfigKind,
		},
		Spec: GlobalRateLimitSpec{
			Limit: Limit{
				Count:    20,
				Duration: time.Second,
				Enabled:  true,
			},
		},
	}
	expectedGlobalSettings := GlobalSettingsConfig{
		ConfigMetadata: ConfigMetadata{
			Version: GlobalSettingsConfigVersion,
			Kind:    GlobalSettingsConfigKind,
			Name:    GlobalSettingsConfigKind,
		},
		Spec: GlobalSettingsSpec{
			ReportOnly: true,
		},
	}
	expectedRateLimits := []RateLimitConfig{
		RateLimitConfig{
			ConfigMetadata: ConfigMetadata{
				Version: RateLimitConfigVersion,
				Kind:    RateLimitConfigKind,
				Name:    "/foo/bar",
			},
			Spec: RateLimitSpec{
				Limit: Limit{
					Count:    5,
					Duration: time.Second,
					Enabled:  true,
				},
				Conditions: Conditions{
					Path: "/foo/bar",
				},
			},
		},
	}
	expectedJails := []JailConfig{
		JailConfig{
			ConfigMetadata: ConfigMetadata{
				Version: JailConfigVersion,
				Kind:    JailConfigKind,
				Name:    "/foo/bar",
			},
			Spec: JailSpec{
				Jail: Jail{
					Limit: Limit{
						Count:    10,
						Duration: time.Minute,
						Enabled:  true,
					},
					BanDuration: time.Hour,
				},
			},
		},
	}

	if err := c.AddWhitelistCidrs(expectedWhitelist); err != nil {
		t.Fatalf("got error: %v", err)
	}

	if err := c.AddBlacklistCidrs(expectedBlacklist); err != nil {
		t.Fatalf("got error: %v", err)
	}

	if err := c.ApplyGlobalRateLimitConfig(expectedGlobalRateLimit); err != nil {
		t.Fatalf("got error: %v", err)
	}

	if err := c.ApplyGlobalSettingsConfig(expectedGlobalSettings); err != nil {
		t.Fatalf("got error: %v", err)
	}

	for _, config := range expectedRateLimits {
		if err := c.ApplyRateLimitConfig(config); err != nil {
			t.Fatalf("got error: %v", err)
		}
	}

	for _, config := range expectedJails {
		if err := c.ApplyJailConfig(config); err != nil {
			t.Fatalf("got error: %v", err)
		}
	}

	gotWhitelist, err := c.FetchWhitelist()
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	gotBlacklist, err := c.FetchBlacklist()
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	gotGlobalRateLimit, err := c.FetchGlobalRateLimitConfig()
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	gotGlobalSettings, err := c.FetchGlobalSettingsConfig()
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	gotRateLimits := c.FetchRateLimitConfigs()
	gotJails := c.FetchJailConfigs()

	if !cmp.Equal(gotWhitelist, expectedWhitelist) {
		t.Errorf("expected: %v received: %v", expectedWhitelist, gotWhitelist)
	}

	if !cmp.Equal(gotBlacklist, expectedBlacklist) {
		t.Errorf("expected: %v received: %v", expectedBlacklist, gotBlacklist)
	}

	if gotGlobalRateLimit != expectedGlobalRateLimit {
		t.Errorf("expected: %v received: %v", expectedGlobalRateLimit, gotGlobalRateLimit)
	}

	if gotGlobalSettings != expectedGlobalSettings {
		t.Errorf("expected: %v received: %v", expectedGlobalSettings, gotGlobalSettings)
	}

	if !cmp.Equal(gotRateLimits, expectedRateLimits) {
		t.Errorf("expected: %v received: %v", expectedRateLimits, gotRateLimits)
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
	expectedGlobalRateLimit := GlobalRateLimitConfig{
		ConfigMetadata: ConfigMetadata{
			Version: GlobalRateLimitConfigVersion,
			Kind:    GlobalRateLimitConfigKind,
			Name:    GlobalRateLimitConfigKind,
		},
		Spec: GlobalRateLimitSpec{
			Limit: Limit{
				Count:    20,
				Duration: time.Second,
				Enabled:  true,
			},
		},
	}
	expectedGlobalSettings := GlobalSettingsConfig{
		ConfigMetadata: ConfigMetadata{
			Version: GlobalSettingsConfigVersion,
			Kind:    GlobalSettingsConfigKind,
			Name:    GlobalSettingsConfigKind,
		},
		Spec: GlobalSettingsSpec{
			ReportOnly: true,
		},
	}

	if err := c.AddWhitelistCidrs(expectedWhitelist); err != nil {
		t.Fatalf("got error: %v", err)
	}

	if err := c.AddBlacklistCidrs(expectedBlacklist); err != nil {
		t.Fatalf("got error: %v", err)
	}

	if err := c.ApplyGlobalRateLimitConfig(expectedGlobalRateLimit); err != nil {
		t.Fatalf("got error: %v", err)
	}

	if err := c.ApplyGlobalSettingsConfig(expectedGlobalSettings); err != nil {
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

	if gotLimit != expectedGlobalRateLimit.Spec.Limit {
		t.Errorf("expected: %v received: %v", expectedGlobalRateLimit.Spec.Limit, gotLimit)
	}

	if gotReportOnly != expectedGlobalSettings.Spec.ReportOnly {
		t.Errorf("expected: %v received: %v", expectedGlobalSettings.Spec.ReportOnly, gotReportOnly)
	}
}

func TestConfStoreRunUpdatesCache(t *testing.T) {
	c, s := newTestConfStore(t)
	defer s.Close()

	expectedWhitelist := parseCIDRs([]string{"10.1.1.1/8"})
	expectedBlacklist := parseCIDRs([]string{"11.1.1.1/8"})
	expectedGlobalRateLimit := GlobalRateLimitConfig{
		ConfigMetadata: ConfigMetadata{
			Version: GlobalRateLimitConfigVersion,
			Kind:    GlobalRateLimitConfigKind,
			Name:    GlobalRateLimitConfigKind,
		},
		Spec: GlobalRateLimitSpec{
			Limit: Limit{
				Count:    40,
				Duration: time.Minute,
				Enabled:  true,
			},
		},
	}
	expectedGlobalSettings := GlobalSettingsConfig{
		ConfigMetadata: ConfigMetadata{
			Version: GlobalSettingsConfigVersion,
			Kind:    GlobalSettingsConfigKind,
			Name:    GlobalSettingsConfigKind,
		},
		Spec: GlobalSettingsSpec{
			ReportOnly: true,
		},
	}

	if err := c.AddWhitelistCidrs(expectedWhitelist); err != nil {
		t.Fatalf("got error: %v", err)
	}

	if err := c.AddBlacklistCidrs(expectedBlacklist); err != nil {
		t.Fatalf("got error: %v", err)
	}

	if err := c.ApplyGlobalRateLimitConfig(expectedGlobalRateLimit); err != nil {
		t.Fatalf("got error: %v", err)
	}

	if err := c.ApplyGlobalSettingsConfig(expectedGlobalSettings); err != nil {
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

	if gotLimit != expectedGlobalRateLimit.Spec.Limit {
		t.Errorf("expected: %v received: %v", expectedGlobalRateLimit.Spec.Limit, gotLimit)
	}

	if gotReportOnly != expectedGlobalSettings.Spec.ReportOnly {
		t.Errorf("expected: %v received: %v", expectedGlobalSettings.Spec.ReportOnly, gotReportOnly)
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

	fooBarRateLimit := RateLimitConfig{
		ConfigMetadata: ConfigMetadata{
			Version: RateLimitConfigVersion,
			Kind:    RateLimitConfigKind,
			Name:    "/foo/bar",
		},
		Spec: RateLimitSpec{
			Limit: Limit{
				Count:    5,
				Duration: time.Second,
				Enabled:  true,
			},
			Conditions: Conditions{
				Path: "/foo/bar",
			},
		},
	}

	fooBazRateLimit := RateLimitConfig{
		ConfigMetadata: ConfigMetadata{
			Version: RateLimitConfigVersion,
			Kind:    RateLimitConfigKind,
			Name:    "/foo/baz",
		},
		Spec: RateLimitSpec{
			Limit: Limit{
				Count:    3,
				Duration: time.Second,
				Enabled:  false,
			},
			Conditions: Conditions{
				Path: "/foo/baz",
			},
		},
	}

	rateLimits := []RateLimitConfig{
		fooBarRateLimit,
		fooBazRateLimit,
	}

	for _, config := range rateLimits {
		if err := c.ApplyRateLimitConfig(config); err != nil {
			t.Fatalf("got error: %v", err)
		}
	}

	config, err := c.FetchRateLimitConfig(fooBarRateLimit.Name)

	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	if !cmp.Equal(config, fooBarRateLimit) {
		t.Errorf("expected: %v, received: %v", fooBarRateLimit, config)
	}

	// Ensure configuration cache is updated after a confSyncInterval
	c.UpdateCachedConf()
	fooBarURL, _ := url.Parse(fooBarRateLimit.Spec.Conditions.Path)
	cachedItem := c.GetRouteRateLimit(*fooBarURL)
	if !cmp.Equal(cachedItem, fooBarRateLimit.Spec.Limit) {
		t.Errorf("expected: %v, received: %v", fooBarRateLimit.Spec.Limit, cachedItem)
	}

	if err := c.DeleteRateLimitConfig(fooBarRateLimit.Name); err != nil {
		t.Fatalf("got error: %v", err)
	}

	// Expect an error since we removed the limits for this route
	rateLimit, err := c.FetchRateLimitConfig(fooBarRateLimit.Name)
	if err == nil {
		t.Fatalf("found rate limit which shouldn't exist")
	}

	// Ensure configuration cache is updated after a confSyncInterval
	c.UpdateCachedConf()
	cachedItem = c.GetRouteRateLimit(*fooBarURL)
	if !cmp.Equal(cachedItem, Limit{}) {
		t.Errorf("expected: %v, received: %v", Limit{}, cachedItem)
	}

	rateLimit, err = c.FetchRateLimitConfig(fooBazRateLimit.Name)
	if err != nil {
		t.Fatalf("got error: %v", err)
	}
	if !cmp.Equal(rateLimit, fooBazRateLimit) {
		t.Errorf("expected: %v, received: %v", fooBazRateLimit, config)
	}
}

func TestConfStoreSetExistingRoute(t *testing.T) {
	c, s := newTestConfStore(t)
	defer s.Close()

	fooBarPath := "/foo/bar"
	originalRateLimit := RateLimitConfig{
		ConfigMetadata: ConfigMetadata{
			Version: RateLimitConfigVersion,
			Kind:    RateLimitConfigKind,
			Name:    fooBarPath,
		},
		Spec: RateLimitSpec{
			Limit: Limit{
				Count:    5,
				Duration: time.Second,
				Enabled:  true,
			},
			Conditions: Conditions{
				Path: fooBarPath,
			},
		},
	}

	if err := c.ApplyRateLimitConfig(originalRateLimit); err != nil {
		t.Fatalf("got error: %v", err)
	}

	newRateLimit := RateLimitConfig{
		ConfigMetadata: ConfigMetadata{
			Version: RateLimitConfigVersion,
			Kind:    RateLimitConfigKind,
			Name:    fooBarPath,
		},
		Spec: RateLimitSpec{
			Limit: Limit{
				Count:    5,
				Duration: time.Second,
				Enabled:  true,
			},
			Conditions: Conditions{
				Path: fooBarPath,
			},
		},
	}

	if err := c.ApplyRateLimitConfig(newRateLimit); err != nil {
		t.Fatalf("got error: %v", err)
	}

	got, err := c.FetchRateLimitConfig(fooBarPath)
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	if !cmp.Equal(newRateLimit, got) {
		t.Errorf("expected: %v, received: %v", newRateLimit, got)
	}
}

func TestConfStoreRemoveNonexistentRoute(t *testing.T) {
	c, s := newTestConfStore(t)
	defer s.Close()
	fooBarPath := "/foo/bar"

	fooBarRateLimit := RateLimitConfig{
		ConfigMetadata: ConfigMetadata{
			Version: RateLimitConfigVersion,
			Kind:    RateLimitConfigKind,
			Name:    fooBarPath,
		},
		Spec: RateLimitSpec{
			Limit: Limit{
				Count:    5,
				Duration: time.Second,
				Enabled:  true,
			},
			Conditions: Conditions{
				Path: fooBarPath,
			},
		},
	}

	fooBazPath := "/foo/baz"
	fooBazRateLimit := RateLimitConfig{
		ConfigMetadata: ConfigMetadata{
			Version: RateLimitConfigVersion,
			Kind:    RateLimitConfigKind,
			Name:    fooBazPath,
		},
		Spec: RateLimitSpec{
			Limit: Limit{
				Count:    3,
				Duration: time.Second,
				Enabled:  false,
			},
			Conditions: Conditions{
				Path: fooBazPath,
			},
		},
	}

	rateLimits := []RateLimitConfig{
		fooBarRateLimit,
		fooBazRateLimit,
	}

	for _, config := range rateLimits {
		if err := c.ApplyRateLimitConfig(config); err != nil {
			t.Fatalf("got error: %v", err)

		}
	}

	nonExistentPath := "/foo/foo"
	names := []string{nonExistentPath, fooBarPath}

	for _, name := range names {
		if err := c.DeleteRateLimitConfig(name); err != nil {
			t.Fatalf("got error: %v", err)
		}
	}

	// Expect an error since we removed the limits for this route
	got, err := c.FetchRateLimitConfig(fooBarRateLimit.Name)
	if err == nil {
		t.Fatalf("expected error fetching route limit which didn't exist")
	}

	got, err = c.FetchRateLimitConfig(fooBazRateLimit.Name)
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	if !cmp.Equal(got, fooBazRateLimit) {
		t.Errorf("expected: %v, received: %v", fooBazRateLimit, got)
	}
}

func TestConfStoreAddRemoveJails(t *testing.T) {
	c, s := newTestConfStore(t)
	defer s.Close()

	fooBarPath := "/foo/bar"
	fooBarJail := JailConfig{
		ConfigMetadata: ConfigMetadata{
			Version: JailConfigVersion,
			Kind:    JailConfigKind,
			Name:    fooBarPath,
		},
		Spec: JailSpec{
			Jail: Jail{
				Limit: Limit{
					Count:    5,
					Duration: time.Second,
					Enabled:  true,
				},
				BanDuration: time.Hour,
			},
			Conditions: Conditions{
				Path: fooBarPath,
			},
		},
	}

	fooBazPath := "/foo/baz"
	fooBazJail := JailConfig{
		ConfigMetadata: ConfigMetadata{
			Version: JailConfigVersion,
			Kind:    JailConfigKind,
			Name:    fooBazPath,
		},
		Spec: JailSpec{
			Jail: Jail{
				Limit: Limit{
					Count:    3,
					Duration: time.Second,
					Enabled:  false,
				},
				BanDuration: time.Hour,
			},
			Conditions: Conditions{
				Path: fooBazPath,
			},
		},
	}

	jails := []JailConfig{
		fooBarJail,
		fooBazJail,
	}

	for _, config := range jails {
		if err := c.ApplyJailConfig(config); err != nil {
			t.Fatalf("got error: %v", err)
		}
	}

	got, err := c.FetchJailConfig(fooBarJail.Name)
	if err != nil {
		t.Fatalf("got error: %v", err)
	}

	if !cmp.Equal(got, fooBarJail) {
		t.Errorf("expected: %v, received: %v", fooBarJail, got)
	}

	fooBarURL, _ := url.Parse(fooBarPath)
	// Ensure configuration cache is updated after a confSyncInterval
	c.UpdateCachedConf()
	cachedItem := c.GetJail(*fooBarURL)
	if !cmp.Equal(cachedItem, fooBarJail.Spec.Jail) {
		t.Errorf("expected: %v, received: %v", fooBarJail, cachedItem)
	}

	if err := c.DeleteJailConfig(fooBarJail.Name); err != nil {
		t.Fatalf("got error: %v", err)
	}

	// Expect an error since we removed the limits for this route
	got, err = c.FetchJailConfig(fooBarJail.Name)
	if err == nil {
		t.Fatalf("expected error fetching route limit which didn't exist")
	}

	// Ensure configuration cache is updated after a confSyncInterval
	c.UpdateCachedConf()
	cachedItem = c.GetJail(*fooBarURL)
	if !cmp.Equal(cachedItem, Jail{}) {
		t.Errorf("expected: %v, received: %v", Jail{}, cachedItem)
	}

	got, err = c.FetchJailConfig(fooBazJail.Name)
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

	fooBarPath := "/foo/bar"
	fooBarJail := JailConfig{
		ConfigMetadata: ConfigMetadata{
			Version: JailConfigVersion,
			Kind:    JailConfigKind,
			Name:    fooBarPath,
		},
		Spec: JailSpec{
			Jail: Jail{
				Limit: Limit{
					Count:    5,
					Duration: time.Second,
					Enabled:  true,
				},
				BanDuration: time.Hour,
			},
			Conditions: Conditions{
				Path: fooBarPath,
			},
		},
	}

	if err := c.ApplyJailConfig(fooBarJail); err != nil {
		t.Fatalf("got error: %v", err)
	}

	newJail := JailConfig{
		ConfigMetadata: ConfigMetadata{
			Version: JailConfigVersion,
			Kind:    JailConfigKind,
			Name:    fooBarPath,
		},
		Spec: JailSpec{
			Jail: Jail{
				Limit: Limit{
					Count:    100,
					Duration: time.Minute,
					Enabled:  false,
				},
				BanDuration: time.Minute,
			},
			Conditions: Conditions{
				Path: fooBarPath,
			},
		},
	}

	if err := c.ApplyJailConfig(newJail); err != nil {
		t.Fatalf("got error: %v", err)
	}

	got, err := c.FetchJailConfig(fooBarJail.Name)
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
