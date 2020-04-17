package e2e

import (
	"flag"
	"fmt"
	"go/build"
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

func TestRateLimit(t *testing.T) {
	resetRedis(*redisAddr)

	config := guardianConfig{
		whitelist:     []string{},
		blacklist:     []string{},
		limitCount:    5,
		limitDuration: time.Minute,
		limitEnabled:  true,
		reportOnly:    false,
	}
	applyGuardianConfig(t, *redisAddr, config)

	for i := 0; i < 10; i++ {
		if len(os.Getenv("SYNC")) == 0 {
			time.Sleep(100 * time.Millisecond) // helps prevents races due asynchronous rate limiting
		}

		res := GET(t, "192.168.1.234", "/")
		res.Body.Close()

		want := 200
		if i >= config.limitCount {
			want = 429
		}

		if res.StatusCode != want {
			t.Fatalf("wanted %v, got %v, iteration %v", want, res.StatusCode, i)
		}
	}
}

func TestWhitelist(t *testing.T) {
	resetRedis(*redisAddr)

	IP := "192.168.1.234"
	CIDR := fmt.Sprintf("%v/32", IP)
	config := guardianConfig{
		whitelist:     []string{CIDR},
		blacklist:     []string{},
		limitCount:    5,
		limitDuration: time.Second,
		limitEnabled:  true,
		reportOnly:    false,
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
		whitelist:     []string{},
		blacklist:     []string{CIDR},
		limitCount:    5,
		limitDuration: time.Second,
		limitEnabled:  true,
		reportOnly:    false,
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

func TestRouteRateLimit(t *testing.T) {
	resetRedis(*redisAddr)
	configFilePath := "./config/routeratelimitconfig.yml"

	config := guardianConfig{
		whitelist:                []string{},
		blacklist:                []string{},
		limitCount:               100,
		limitDuration:            time.Second,
		limitEnabled:             false,
		reportOnly:               false,
		routeRateLimitConfigPath: configFilePath,
	}
	applyGuardianConfig(t, *redisAddr, config)

	rrlConfig := guardian.RouteRateLimitConfig{}
	rrlConfigBytes, err := ioutil.ReadFile(configFilePath)
	if err != nil {
		t.Fatalf("unable to read config file: %v", err)
	}
	err = yaml.Unmarshal(rrlConfigBytes, &rrlConfig)
	if err != nil {
		t.Fatalf("error unmarshaling expected result string: %v", err)
	}

	for _, routeRateLimit := range rrlConfig.RouteRatelimits {
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

func TestSetRouteRateLimits(t *testing.T) {
	resetRedis(*redisAddr)
	configFilePath := "./config/routeratelimitconfig.yml"
	config := guardianConfig{
		routeRateLimitConfigPath: configFilePath,
	}
	applyGuardianConfig(t, *redisAddr, config)
	getCmd := "get-route-rate-limits"
	resStr := runGuardianCLI(t, *redisAddr, getCmd)
	expectedResStr, err := ioutil.ReadFile(configFilePath)

	res := guardian.RouteRateLimitConfig{}
	expectedRes := guardian.RouteRateLimitConfig{}
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
	for _, entry := range expectedRes.RouteRatelimits {
		expectedResSet[entry.Route] = entry.Limit
	}
	for _, entry := range res.RouteRatelimits {
		resSet[entry.Route] = entry.Limit
	}

	if !cmp.Equal(resSet, expectedResSet) {
		t.Fatalf("expected: %v, received: %v", expectedResSet, resSet)
	}
}

func TestRemoveRouteRateLimits(t *testing.T) {
	resetRedis(*redisAddr)
	configFilePath := "./config/routeratelimitconfig.yml"
	config := guardianConfig{
		routeRateLimitConfigPath: configFilePath,
	}
	applyGuardianConfig(t, *redisAddr, config)
	rmCmd := "remove-route-rate-limits"
	runGuardianCLI(t, *redisAddr, rmCmd, "/foo/bar,/foo/baz")

	getCmd := "get-route-rate-limits"
	resStr := runGuardianCLI(t, *redisAddr, getCmd)

	res := guardian.RouteRateLimitConfig{}
	err := yaml.Unmarshal([]byte(resStr), &res)
	if err != nil {
		t.Fatalf("error unmarshaling result string: %v", err)
	}

	if len(res.RouteRatelimits) != 0 {
		t.Fatalf("expected route rate limits to be empty after removing them")
	}
}


func TestJails(t *testing.T) {
	resetRedis(*redisAddr)
	configFilePath := "./config/jailconfig.yml"

	config := guardianConfig{
		whitelist:                []string{"192.168.1.1/32"},
		blacklist:                []string{},
		limitCount:               5,
		limitDuration:            time.Minute,
		limitEnabled:             false,
		reportOnly:               false,
		routeRateLimitConfigPath: "",
		jailConfigPath:           configFilePath,
	}

	applyGuardianConfig(t, *redisAddr, config)
	jailConfig := &guardian.JailConfig{}
	jailConfigContents, err := ioutil.ReadFile(config.jailConfigPath)
	if err != nil {
		t.Fatalf("unable to read config file: %v", err)
	}
	err = yaml.Unmarshal(jailConfigContents, jailConfig)
	if err != nil {
		t.Fatalf("error unmarhsaling config file contents: %v", err)
	}

	banned := false
	// Assumes that any BanDuration in the Jail Config is greater than the time it takes
	// to execute this particular test.
	for _, j := range jailConfig.Jails {
		for i := uint64(0); i < j.Jail.Limit.Count+5; i++ {
			if len(os.Getenv("SYNC")) == 0 {
				time.Sleep(100 * time.Millisecond) // helps prevents races due asynchronous rate limiting
			}

			res := GET(t, "192.168.1.43", j.Route)
			whitelistedRes := GET(t, "192.168.1.1", j.Route)
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
	}

}

func TestSetJails(t *testing.T) {
	resetRedis(*redisAddr)
	configFilePath := "./config/jailconfig.yml"
	config := guardianConfig{
		jailConfigPath:           configFilePath,
	}
	applyGuardianConfig(t, *redisAddr, config)

	getCmd := "get-jails"
	resStr := runGuardianCLI(t, *redisAddr, getCmd)
	expectedResStr, err := ioutil.ReadFile(configFilePath)

	res := guardian.JailConfig{}
	expectedRes := guardian.JailConfig{}
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

func TestRemoveJail(t *testing.T) {
	resetRedis(*redisAddr)
	configFilePath := "./config/jailconfig.yml"
	config := guardianConfig{
		jailConfigPath: configFilePath,
	}
	applyGuardianConfig(t, *redisAddr, config)
	rmCmd := "remove-jails"
	runGuardianCLI(t, *redisAddr, rmCmd, "/foo/bar,/foo/baz")

	getCmd := "get-jails"
	resStr := runGuardianCLI(t, *redisAddr, getCmd)

	res := guardian.JailConfig{}
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
	whitelist                []string
	blacklist                []string
	limitCount               int
	limitDuration            time.Duration
	limitEnabled             bool
	reportOnly               bool
	routeRateLimitConfigPath string
	jailConfigPath           string
}

func applyGuardianConfig(t *testing.T, redisAddr string, c guardianConfig) {
	t.Helper()

	runGuardianCLI(t, redisAddr, "set-limit", strconv.Itoa(c.limitCount), c.limitDuration.String(), strconv.FormatBool(c.limitEnabled))
	runGuardianCLI(t, redisAddr, "set-report-only", strconv.FormatBool(c.reportOnly))

	clearXList(t, redisAddr, "blacklist")
	clearXList(t, redisAddr, "whitelist")

	if len(c.whitelist) > 0 {
		runGuardianCLI(t, redisAddr, "add-whitelist", strings.Join(c.whitelist, " "))
	}

	if len(c.blacklist) > 0 {
		runGuardianCLI(t, redisAddr, "add-blacklist", strings.Join(c.blacklist, " "))
	}

	if len(c.routeRateLimitConfigPath) > 0 {
		runGuardianCLI(t, redisAddr, "set-route-rate-limits", c.routeRateLimitConfigPath)
	}

	if len(c.jailConfigPath) > 0 {
		runGuardianCLI(t, redisAddr, "set-jails", c.jailConfigPath)
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
	output, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("error running guardian-cli: %v %v", err, string(output))
	}

	return string(output)
}
