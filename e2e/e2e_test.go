package e2e

import (
	"flag"
	"fmt"
	"go/build"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dollarshaveclub/guardian/pkg/guardian"
	"github.com/google/go-cmp/cmp"

	"github.com/go-redis/redis"
	yaml "gopkg.in/yaml.v2"
)

var redisAddr = flag.String("redis-addr", "localhost:6379", "redis address")
var envoyAddr = flag.String("envoy-addr", "localhost:8080", "envoy address")

func TestWhitelist(t *testing.T) {
	resetRedis(*redisAddr)

	IP := "192.168.1.234"
	CIDR := fmt.Sprintf("%v/32", IP)
	config := guardianConfig{
		whitelist:                 []string{CIDR},
		blacklist:                 []string{},
		globalRateLimitConfigPath: "./config/TestWhitelist/globalratelimitconfig.yml",
		globalSettingsConfigPath:  "./config/TestWhitelist/globalsettingsconfig.yml",
	}
	applyGuardianConfig(t, *redisAddr, config)

	for i := 0; i < 10; i++ {
		res := GET(t, IP, "/")
		res.Body.Close()

		want := 200
		if res.StatusCode != want {
			t.Fatalf("wanted %v, got %v", want, res.StatusCode)
		}
	}
}

func TestBlacklist(t *testing.T) {
	resetRedis(*redisAddr)

	IP := "192.168.1.234"
	CIDR := fmt.Sprintf("%v/32", IP)
	config := guardianConfig{
		whitelist:                 []string{},
		blacklist:                 []string{CIDR},
		globalRateLimitConfigPath: "./config/TestBlacklist/globalratelimitconfig.yml",
		globalSettingsConfigPath:  "./config/TestBlacklist/globalsettingsconfig.yml",
	}
	applyGuardianConfig(t, *redisAddr, config)

	for i := 0; i < 10; i++ {
		res := GET(t, IP, "/")
		res.Body.Close()

		want := 429
		if res.StatusCode != want {
			t.Fatalf("wanted %v, got %v", want, res.StatusCode)
		}
	}
}

func TestGlobalRateLimit(t *testing.T) {
	resetRedis(*redisAddr)

	guardianConfig := guardianConfig{
		whitelist:                 []string{},
		blacklist:                 []string{},
		globalRateLimitConfigPath: "./config/TestGlobalRateLimit/globalratelimitconfig.yml",
		globalSettingsConfigPath:  "./config/TestGlobalRateLimit/globalsettingsconfig.yml",
	}
	applyGuardianConfig(t, *redisAddr, guardianConfig)

	file, err := os.Open(guardianConfig.globalRateLimitConfigPath)
	if err != nil {
		t.Fatalf("error opening config file: %v", err)
	}
	defer file.Close()
	config := guardian.GlobalRateLimitConfig{}
	err = yaml.NewDecoder(file).Decode(&config)
	if err != nil {
		t.Fatalf("error decoding yaml: %v", err)
	}

	for i := uint64(0); i < 10; i++ {
		if len(os.Getenv("SYNC")) == 0 {
			time.Sleep(100 * time.Millisecond) // helps prevents races due asynchronous rate limiting
		}

		res := GET(t, "192.168.1.234", "/")
		res.Body.Close()

		want := 200
		if i >= config.Spec.Limit.Count {
			want = 429
		}

		if res.StatusCode != want {
			t.Fatalf("wanted %v, got %v, iteration %v", want, res.StatusCode, i)
		}
	}
}

func TestRateLimit(t *testing.T) {
	resetRedis(*redisAddr)

	guardianConfig := guardianConfig{
		whitelist:                 []string{},
		blacklist:                 []string{},
		globalRateLimitConfigPath: "./config/TestRateLimit/globalratelimitconfig.yml",
		globalSettingsConfigPath:  "./config/TestRateLimit/globalsettingsconfig.yml",
		rateLimitConfigPath:       "./config/TestRateLimit/ratelimitconfig.yml",
	}
	applyGuardianConfig(t, *redisAddr, guardianConfig)

	file, err := os.Open(guardianConfig.rateLimitConfigPath)
	if err != nil {
		t.Fatalf("error opening config file: %v", err)
	}
	defer file.Close()
	dec := yaml.NewDecoder(file)
	for {
		config := guardian.RateLimitConfig{}
		err := dec.Decode(&config)
		if err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("error decoding yaml: %v", err)
		}
		for i := uint64(0); i < config.Spec.Limit.Count+5; i++ {
			if len(os.Getenv("SYNC")) == 0 {
				time.Sleep(100 * time.Millisecond) // helps prevents races due asynchronous rate limiting
			}

			res := GET(t, "192.168.1.234", config.Spec.Conditions.Path)
			res.Body.Close()

			want := 200
			if i >= config.Spec.Limit.Count && config.Spec.Limit.Enabled {
				want = 429
			}

			if res.StatusCode != want {
				t.Fatalf("wanted %v, got %v, iteration %v", want, res.StatusCode, i)
			}
		}
	}
}

func TestJails(t *testing.T) {
	resetRedis(*redisAddr)
	whitelistedIP := "192.168.1.1"

	guardianConfig := guardianConfig{
		whitelist:                 []string{whitelistedIP + "/32"},
		blacklist:                 []string{},
		globalRateLimitConfigPath: "./config/TestJails/globalratelimitconfig.yml",
		globalSettingsConfigPath:  "./config/TestJails/globalsettingsconfig.yml",
		jailConfigPath:            "./config/TestJails/jailconfig.yml",
	}

	applyGuardianConfig(t, *redisAddr, guardianConfig)

	file, err := os.Open(guardianConfig.jailConfigPath)
	if err != nil {
		t.Fatalf("error opening config file: %v", err)
	}
	defer file.Close()
	dec := yaml.NewDecoder(file)

	// Assumes that any BanDuration in the Jail Config is greater than the time it takes
	// to execute this particular test.
	for {
		config := guardian.JailConfig{}
		err := dec.Decode(&config)
		if err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("error decoding yaml: %v", err)
		}
		banned := false
		resetRedis(*redisAddr)
		applyGuardianConfig(t, *redisAddr, guardianConfig)
		for i := uint64(0); i < config.Spec.Limit.Count+1; i++ {
			if len(os.Getenv("SYNC")) == 0 {
				time.Sleep(150 * time.Millisecond) // helps prevents races due asynchronous rate limiting
			}

			res := GET(t, "192.168.1.43", config.Spec.Conditions.Path)
			whitelistedRes := GET(t, whitelistedIP, config.Spec.Conditions.Path)
			res.Body.Close()
			whitelistedRes.Body.Close()

			want := 200
			if (i >= config.Spec.Limit.Count && config.Spec.Limit.Enabled) || banned {
				banned = true
				want = 429
			}

			if res.StatusCode != want {
				t.Fatalf("wanted %v, got %v, iteration %v, route: %v", want, res.StatusCode, i, config.Spec.Conditions.Path)
			}

			if whitelistedRes.StatusCode != 200 {
				t.Fatalf("whitelisted ip received unexpected status code: wanted %v, got %v, iteration %d, route: %v", 200, whitelistedRes.StatusCode, i, config.Spec.Conditions.Path)
			}
		}
		if config.Spec.Limit.Enabled {
			t.Logf("sleeping for ban_duration: %v + 2 seconds to ensure the prisoner is removed", config.Spec.BanDuration)
			time.Sleep(config.Spec.BanDuration)
			time.Sleep(2 * time.Second) // ensure that we sleep for an additional confUpdateInterval so that the configuration is updated
			res := GET(t, "192.168.1.43", config.Spec.Conditions.Path)
			if res.StatusCode != 200 {
				t.Fatalf("prisoner was never removed, received unexpected status code: %d, %v", res.StatusCode, config.Spec.Jail)
			}
		}
	}
}

func TestDeleteRateLimit(t *testing.T) {
	resetRedis(*redisAddr)

	config := guardianConfig{
		whitelist:                 []string{},
		blacklist:                 []string{},
		globalRateLimitConfigPath: "./config/TestDeleteRateLimit/globalratelimitconfig.yml",
		globalSettingsConfigPath:  "./config/TestDeleteRateLimit/globalsettingsconfig.yml",
		rateLimitConfigPath:       "./config/TestDeleteRateLimit/ratelimitconfig.yml",
	}
	applyGuardianConfig(t, *redisAddr, config)
	delCmd := "delete"
	runGuardianCLI(t, *redisAddr, delCmd, "RateLimit", "/foo/bar")
	runGuardianCLI(t, *redisAddr, delCmd, "RateLimit", "/foo/baz")

	getCmd := "get"
	resStr := runGuardianCLI(t, *redisAddr, getCmd, "RateLimit")

	if len(resStr) != 0 {
		t.Fatalf("get RateLimit returned non-empty output %v", resStr)
	}
}

func TestDeleteJail(t *testing.T) {
	resetRedis(*redisAddr)

	config := guardianConfig{
		whitelist:                 []string{},
		blacklist:                 []string{},
		globalRateLimitConfigPath: "./config/TestDeleteJail/globalratelimitconfig.yml",
		globalSettingsConfigPath:  "./config/TestDeleteJail/globalsettingsconfig.yml",
		jailConfigPath:            "./config/TestDeleteJail/jailconfig.yml",
	}
	applyGuardianConfig(t, *redisAddr, config)
	delCmd := "delete"
	runGuardianCLI(t, *redisAddr, delCmd, "Jail", "/foo/bar")
	runGuardianCLI(t, *redisAddr, delCmd, "Jail", "/foo/baz")

	getCmd := "get"
	resStr := runGuardianCLI(t, *redisAddr, getCmd, "Jail")

	if len(resStr) != 0 {
		t.Fatalf("get Jail returned non-empty output %v", resStr)
	}
}

func TestRateLimitDeprecated(t *testing.T) {
	resetRedis(*redisAddr)

	config := guardianConfig{
		whitelist:               []string{},
		blacklist:               []string{},
		limitCountDeprecated:    5,
		limitDurationDeprecated: time.Minute,
		limitEnabledDeprecated:  true,
		reportOnlyDeprecated:    false,
	}
	applyGuardianConfigDeprecated(t, *redisAddr, config)

	for i := 0; i < 10; i++ {
		if len(os.Getenv("SYNC")) == 0 {
			time.Sleep(100 * time.Millisecond) // helps prevents races due asynchronous rate limiting
		}

		res := GET(t, "192.168.1.234", "/")
		res.Body.Close()

		want := 200
		if i >= config.limitCountDeprecated {
			want = 429
		}

		if res.StatusCode != want {
			t.Fatalf("wanted %v, got %v, iteration %v", want, res.StatusCode, i)
		}
	}
}

func TestRouteRateLimitDeprecated(t *testing.T) {
	resetRedis(*redisAddr)
	config := guardianConfig{
		whitelist:                          []string{},
		blacklist:                          []string{},
		limitCountDeprecated:               100,
		limitDurationDeprecated:            time.Second,
		limitEnabledDeprecated:             false,
		reportOnlyDeprecated:               false,
		routeRateLimitConfigPathDeprecated: "./config/TestRouteRateLimitDeprecated/routeratelimitconfig.yml",
	}
	applyGuardianConfigDeprecated(t, *redisAddr, config)

	rrlConfig := guardian.RouteRateLimitConfigDeprecated{}
	rrlConfigBytes, err := ioutil.ReadFile(config.routeRateLimitConfigPathDeprecated)
	if err != nil {
		t.Fatalf("unable to read config file: %v", err)
	}
	err = yaml.Unmarshal(rrlConfigBytes, &rrlConfig)
	if err != nil {
		t.Fatalf("error unmarshaling expected result string: %v", err)
	}

	for _, routeRateLimit := range rrlConfig.RouteRateLimits {
		for i := uint64(0); i < routeRateLimit.Limit.Count+5; i++ {
			if len(os.Getenv("SYNC")) == 0 {
				time.Sleep(100 * time.Millisecond) // helps prevents races due asynchronous rate limiting
			}

			res := GET(t, "192.168.1.234", routeRateLimit.Route)
			res.Body.Close()

			want := 200
			if i >= routeRateLimit.Limit.Count && routeRateLimit.Limit.Enabled {
				want = 429
			}

			if res.StatusCode != want {
				t.Fatalf("wanted %v, got %v, iteration %v", want, res.StatusCode, i)
			}
		}
	}
}

func TestJailsDeprecated(t *testing.T) {
	resetRedis(*redisAddr)
	whitelistedIP := "192.168.1.1"

	config := guardianConfig{
		whitelist:                []string{whitelistedIP + "/32"},
		blacklist:                []string{},
		limitCountDeprecated:     5,
		limitDurationDeprecated:  time.Minute,
		limitEnabledDeprecated:   false,
		reportOnlyDeprecated:     false,
		jailConfigPathDeprecated: "./config/TestJailsDeprecated/jailconfig.yml",
	}

	applyGuardianConfigDeprecated(t, *redisAddr, config)
	jailConfig := &guardian.JailConfigDeprecated{}
	jailConfigContents, err := ioutil.ReadFile(config.jailConfigPathDeprecated)
	if err != nil {
		t.Fatalf("unable to read config file: %v", err)
	}
	err = yaml.Unmarshal(jailConfigContents, jailConfig)
	if err != nil {
		t.Fatalf("error unmarhsaling config file contents: %v", err)
	}

	// Assumes that any BanDuration in the Jail Config is greater than the time it takes
	// to execute this particular test.
	for _, j := range jailConfig.Jails {
		banned := false
		resetRedis(*redisAddr)
		applyGuardianConfigDeprecated(t, *redisAddr, config)
		for i := uint64(0); i < j.Jail.Limit.Count+1; i++ {
			if len(os.Getenv("SYNC")) == 0 {
				time.Sleep(150 * time.Millisecond) // helps prevents races due asynchronous rate limiting
			}

			res := GET(t, "192.168.1.43", j.Route)
			whitelistedRes := GET(t, whitelistedIP, j.Route)
			res.Body.Close()
			whitelistedRes.Body.Close()

			want := 200
			if (i >= j.Jail.Limit.Count && j.Jail.Limit.Enabled) || banned {
				banned = true
				want = 429
			}

			if res.StatusCode != want {
				t.Fatalf("wanted %v, got %v, iteration %v, route: %v", want, res.StatusCode, i, j.Route)
			}

			if whitelistedRes.StatusCode != 200 {
				t.Fatalf("whitelisted ip received unexpected status code: wanted %v, got %v, iteration %d, route: %v", 200, whitelistedRes.StatusCode, i, j.Route)
			}
		}
		if j.Jail.Limit.Enabled {
			t.Logf("sleeping for ban_duration: %v + 2 seconds to ensure the prisoner is removed", j.Jail.BanDuration)
			time.Sleep(j.Jail.BanDuration)
			time.Sleep(2 * time.Second) // ensure that we sleep for an additional confUpdateInterval so that the configuration is updated
			res := GET(t, "192.168.1.43", j.Route)
			if res.StatusCode != 200 {
				t.Fatalf("prisoner was never removed, received unexpected status code: %d, %v", res.StatusCode, j.Jail)
			}
		}
	}
}

func TestSetRouteRateLimitsDeprecated(t *testing.T) {
	resetRedis(*redisAddr)
	config := guardianConfig{
		whitelist:                          []string{},
		blacklist:                          []string{},
		routeRateLimitConfigPathDeprecated: "./config/TestSetRouteRateLimitsDeprecated/routeratelimitconfig.yml",
	}
	applyGuardianConfigDeprecated(t, *redisAddr, config)
	getCmd := "get-route-rate-limits"
	resStr := runGuardianCLI(t, *redisAddr, getCmd)
	expectedResStr, err := ioutil.ReadFile(config.routeRateLimitConfigPathDeprecated)

	res := guardian.RouteRateLimitConfigDeprecated{}
	expectedRes := guardian.RouteRateLimitConfigDeprecated{}
	err = yaml.Unmarshal([]byte(resStr), &res)
	if err != nil {
		t.Fatalf("error unmarshaling result string: %v", err)
	}
	err = yaml.Unmarshal(expectedResStr, &expectedRes)
	if err != nil {
		t.Fatalf("error unmarshaling expected result string: %v", err)
	}

	// Since the ordering of the slice returned from the cli can be different
	// than the original config, we just want to verify that both configs contain
	// the same entries in no particular order.
	expectedResSet := make(map[string]guardian.Limit)
	resSet := make(map[string]guardian.Limit)
	for _, entry := range expectedRes.RouteRateLimits {
		expectedResSet[entry.Route] = entry.Limit
	}
	for _, entry := range res.RouteRateLimits {
		resSet[entry.Route] = entry.Limit
	}

	if !cmp.Equal(resSet, expectedResSet) {
		t.Fatalf("expected: %v, received: %v", expectedResSet, resSet)
	}
}

func TestRemoveRouteRateLimitsDeprecated(t *testing.T) {
	resetRedis(*redisAddr)
	// TODO: Set Global configs?
	config := guardianConfig{
		whitelist:                          []string{},
		blacklist:                          []string{},
		routeRateLimitConfigPathDeprecated: "./config/TestRemoveRouteRateLimitsDeprecated/routeratelimitconfig.yml",
	}
	applyGuardianConfigDeprecated(t, *redisAddr, config)
	rmCmd := "remove-route-rate-limits"
	runGuardianCLI(t, *redisAddr, rmCmd, "/foo/bar,/foo/baz")

	getCmd := "get-route-rate-limits"
	resStr := runGuardianCLI(t, *redisAddr, getCmd)

	res := guardian.RouteRateLimitConfigDeprecated{}
	err := yaml.Unmarshal([]byte(resStr), &res)
	if err != nil {
		t.Fatalf("error unmarshaling result string: %v", err)
	}

	if len(res.RouteRateLimits) != 0 {
		t.Fatalf("expected route rate limits to be empty after removing them")
	}
}

func TestSetJailsDeprecated(t *testing.T) {
	resetRedis(*redisAddr)
	config := guardianConfig{
		whitelist:                []string{},
		blacklist:                []string{},
		jailConfigPathDeprecated: "./config/TestSetJailsDeprecated/jailconfig.yml",
	}
	applyGuardianConfigDeprecated(t, *redisAddr, config)

	getCmd := "get-jails"
	resStr := runGuardianCLI(t, *redisAddr, getCmd)
	t.Logf("Got result: %v", resStr)
	expectedResStr, err := ioutil.ReadFile(config.jailConfigPathDeprecated)
	res := guardian.JailConfigDeprecated{}
	expectedRes := guardian.JailConfigDeprecated{}
	err = yaml.Unmarshal([]byte(resStr), &res)
	if err != nil {
		t.Fatalf("error unmarshaling result string: %v", err)
	}
	err = yaml.Unmarshal(expectedResStr, &expectedRes)
	if err != nil {
		t.Fatalf("error unmarshaling expected result string: %v", err)
	}

	// Since the ordering of the slice returned from the cli can be different
	// than the original config, we just want to verify that both configs contain
	// the same entries in no particular order.
	expectedResSet := make(map[string]guardian.Jail)
	resSet := make(map[string]guardian.Jail)
	for _, entry := range expectedRes.Jails {
		expectedResSet[entry.Route] = entry.Jail
	}
	for _, entry := range res.Jails {
		resSet[entry.Route] = entry.Jail
	}

	if !cmp.Equal(resSet, expectedResSet) {
		t.Fatalf("expected: %v, received: %v", expectedResSet, resSet)
	}
}

func TestRemoveJailDeprecated(t *testing.T) {
	resetRedis(*redisAddr)

	config := guardianConfig{
		whitelist:                []string{},
		blacklist:                []string{},
		jailConfigPathDeprecated: "./config/TestRemoveJailDeprecated/jailconfig.yml",
	}
	applyGuardianConfigDeprecated(t, *redisAddr, config)
	rmCmd := "remove-jails"
	runGuardianCLI(t, *redisAddr, rmCmd, "/foo/bar,/foo/baz")

	getCmd := "get-jails"
	resStr := runGuardianCLI(t, *redisAddr, getCmd)

	res := guardian.JailConfigDeprecated{}
	err := yaml.Unmarshal([]byte(resStr), &res)
	if err != nil {
		t.Fatalf("error unmarshaling result string: %v", err)
	}

	if len(res.Jails) != 0 {
		t.Fatalf("expected route rate limits to be empty after removing them")
	}
}

func GET(t *testing.T, sourceIP string, path string) *http.Response {
	t.Helper()

	url := fmt.Sprintf("http://%v%v", *envoyAddr, path)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatalf("error making request %v: %v", url, err)
	}
	req.Header.Add("X-Forwarded-For", fmt.Sprintf("%v, 10.0.0.123", sourceIP))

	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("error running GET %v: %v", url, err)
	}

	return res
}

func resetRedis(redisAddr string) {
	redisOpts := &redis.Options{
		Addr: redisAddr,
	}

	redis := redis.NewClient(redisOpts)
	redis.FlushAll()
}

type guardianConfig struct {
	whitelist                 []string
	blacklist                 []string
	globalRateLimitConfigPath string
	globalSettingsConfigPath  string
	rateLimitConfigPath       string
	jailConfigPath            string
	// Fields associated with deprecated CLI
	limitCountDeprecated               int
	limitDurationDeprecated            time.Duration
	limitEnabledDeprecated             bool
	reportOnlyDeprecated               bool
	routeRateLimitConfigPathDeprecated string
	jailConfigPathDeprecated           string
}

func applyGuardianConfig(t *testing.T, redisAddr string, c guardianConfig) {
	t.Helper()

	if len(c.globalRateLimitConfigPath) > 0 {
		runGuardianCLI(t, redisAddr, "apply", c.globalRateLimitConfigPath)
	}

	if len(c.globalSettingsConfigPath) > 0 {
		runGuardianCLI(t, redisAddr, "apply", c.globalSettingsConfigPath)
	}

	clearXList(t, redisAddr, "blacklist")
	clearXList(t, redisAddr, "whitelist")

	if len(c.whitelist) > 0 {
		runGuardianCLI(t, redisAddr, "add-whitelist", strings.Join(c.whitelist, " "))
	}

	if len(c.blacklist) > 0 {
		runGuardianCLI(t, redisAddr, "add-blacklist", strings.Join(c.blacklist, " "))
	}

	if len(c.rateLimitConfigPath) > 0 {
		runGuardianCLI(t, redisAddr, "apply", c.rateLimitConfigPath)
	}

	if len(c.jailConfigPath) > 0 {
		runGuardianCLI(t, redisAddr, "apply", c.jailConfigPath)
	}

	time.Sleep(2 * time.Second)
}

func applyGuardianConfigDeprecated(t *testing.T, redisAddr string, c guardianConfig) {
	t.Helper()

	runGuardianCLI(
		t,
		redisAddr,
		"set-limit",
		strconv.Itoa(c.limitCountDeprecated),
		c.limitDurationDeprecated.String(),
		strconv.FormatBool(c.limitEnabledDeprecated),
	)

	runGuardianCLI(t, redisAddr, "set-report-only", strconv.FormatBool(c.reportOnlyDeprecated))

	clearXList(t, redisAddr, "blacklist")
	clearXList(t, redisAddr, "whitelist")

	if len(c.whitelist) > 0 {
		runGuardianCLI(t, redisAddr, "add-whitelist", strings.Join(c.whitelist, " "))
	}

	if len(c.blacklist) > 0 {
		runGuardianCLI(t, redisAddr, "add-blacklist", strings.Join(c.blacklist, " "))
	}

	if len(c.routeRateLimitConfigPathDeprecated) > 0 {
		runGuardianCLI(t, redisAddr, "set-route-rate-limits", c.routeRateLimitConfigPathDeprecated)
	}

	if len(c.jailConfigPathDeprecated) > 0 {
		runGuardianCLI(t, redisAddr, "set-jails", c.jailConfigPathDeprecated)
	}

	time.Sleep(2 * time.Second)
}

// listType one of blacklist whitelist
func clearXList(t *testing.T, redisAddr string, listType string) {
	t.Helper()
	listCmd := fmt.Sprintf("get-%v", listType)
	currList := runGuardianCLI(t, redisAddr, listCmd)
	currList = strings.Trim(currList, "\n")
	currList = strings.Trim(currList, " ")
	if len(currList) == 0 {
		return
	}

	currList = strings.Join(strings.Split(currList, "\n"), " ")
	clearCmd := fmt.Sprintf("remove-%v", listType)
	runGuardianCLI(t, redisAddr, clearCmd, currList)
}

func runGuardianCLI(t *testing.T, redisAddr string, command string, args ...string) string {
	t.Helper()
	GOPATH := build.Default.GOPATH

	cliPath := filepath.Join(GOPATH, "bin", "guardian-cli")
	if len(GOPATH) == 0 {
		var err error
		cliPath, err = exec.LookPath("guardian-cli")
		if err != nil {
			t.Fatal("could not find guardian-cli. Is it built and in your path?")
		}
	}

	cmdArgs := append([]string{command, "-r", redisAddr}, args...)
	c := exec.Command(cliPath, cmdArgs...)
	output, err := c.Output()
	if err != nil {
		t.Fatalf("error running guardian-cli: %v %v", err, string(output))
	}

	return string(output)
}
