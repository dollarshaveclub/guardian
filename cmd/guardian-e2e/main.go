package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dollarshaveclub/guardian/pkg/guardian"

	"github.com/go-redis/redis"
	"gopkg.in/alecthomas/kingpin.v2"
	yaml "gopkg.in/yaml.v2"
)

var redisAddr *string = kingpin.Flag("redis-addr", "host:port.").Default("0.0.0.0:6379").String()
var envoyAddr *string = kingpin.Flag("envoy-addr", "host:port.").Default("0.0.0.0:10000").String()

const defaultAsyncCounterTimeout = 300 * time.Millisecond

func main() {
	kingpin.Parse()
	errored := false
	if err := TestWhitelist(); err != nil {
		errored = true
		log.Printf("TestWhitelist: %v", err)
	}
	if err := TestBlacklist(); err != nil {
		errored = true
		log.Printf("TestBlacklist: %v", err)
	}
	if err := TestGlobalRateLimit(); err != nil {
		errored = true
		log.Printf("TestGlobalRateLimit: %v", err)
	}
	if err := TestRateLimit(); err != nil {
		errored = true
		log.Printf("TestRateLimit: %v", err)
	}
	if err := TestJails(); err != nil {
		errored = true
		log.Printf("TestJails: %v", err)
	}
	if err := TestDeleteRateLimit(); err != nil {
		errored = true
		log.Printf("TestDeleteRateLimit: %v", err)
	}
	if err := TestDeleteRateLimit(); err != nil {
		errored = true
		log.Printf("TestDeleteJail: %v", err)
	}
	if errored {
		os.Exit(1)
	}
}

func TestWhitelist() error {
	resetRedis(*redisAddr)
	IP := pseudoRandomIPV4Address()
	CIDR := fmt.Sprintf("%v/32", IP)
	config := guardianConfig{
		whitelist: []string{CIDR},
		blacklist: []string{},
		globalRateLimitConfig: `
version: "v0"
kind: GlobalRateLimit
name: GlobalRateLimit
description: GlobalRateLimit
globalRateLimitSpec:
 limit:
   count: 5
   duration: 1s
   enabled: true`,
		globalSettingsConfig: `
version: "v0"
kind: GlobalSettings
name: GlobalSettings
description: GlobalSettings
globalSettingsSpec:
  reportOnly: false`,
	}
	applyGuardianConfig(*redisAddr, config)

	for i := 0; i < 10; i++ {
		res, err := GET(IP, "/")
		if err != nil {
			return err
		}
		res.Body.Close()

		want := 200
		if res.StatusCode != want {
			return fmt.Errorf("wanted %v, got %v", want, res.StatusCode)
		}
	}
	return nil
}

func TestBlacklist() error {
	resetRedis(*redisAddr)
	IP := pseudoRandomIPV4Address()
	CIDR := fmt.Sprintf("%v/32", IP)
	config := guardianConfig{
		whitelist: []string{},
		blacklist: []string{CIDR},
		globalRateLimitConfig: `
version: "v0"
kind: GlobalRateLimit
name: GlobalRateLimit
description: GlobalRateLimit
globalRateLimitSpec:
 limit:
   count: 5
   duration: 1s
   enabled: true`,
		globalSettingsConfig: `
version: "v0"
kind: GlobalSettings
name: GlobalSettings
description: GlobalSettings
globalSettingsSpec:
  reportOnly: false`,
	}
	applyGuardianConfig(*redisAddr, config)

	for i := 0; i < 10; i++ {
		res, err := GET(IP, "/")
		if err != nil {
			return err
		}
		res.Body.Close()

		want := 429
		if res.StatusCode != want {
			return fmt.Errorf("wanted %v, got %v", want, res.StatusCode)
		}
	}
	return nil
}

func TestGlobalRateLimit() error {
	resetRedis(*redisAddr)
	IP := pseudoRandomIPV4Address()
	guardianConfig := guardianConfig{
		whitelist: []string{},
		blacklist: []string{},
		globalRateLimitConfig: `
version: "v0"
kind: GlobalRateLimit
name: GlobalRateLimit
description: GlobalRateLimit
globalRateLimitSpec:
 limit:
   count: 5
   duration: 1m
   enabled: true`,
		globalSettingsConfig: `
version: "v0"
kind: GlobalSettings
name: GlobalSettings
description: GlobalSettings
globalSettingsSpec:
  reportOnly: false`,
	}

	applyGuardianConfig(*redisAddr, guardianConfig)

	config := guardian.GlobalRateLimitConfig{}
	if err := yaml.Unmarshal([]byte(guardianConfig.globalRateLimitConfig), &config); err != nil {
		return fmt.Errorf("error decoding yaml: %v", err)
	}

	for i := uint64(0); i < 10; i++ {
		if len(os.Getenv("SYNC")) == 0 {
			time.Sleep(defaultAsyncCounterTimeout) // helps prevents races due asynchronous rate limiting
		}

		res, err := GET(IP, "/")
		if err != nil {
			return err
		}
		res.Body.Close()

		want := 200
		if i >= config.Spec.Limit.Count {
			want = 429
		}

		if res.StatusCode != want {
			return fmt.Errorf("wanted %v, got %v, iteration %v", want, res.StatusCode, i)
		}
	}
	return nil
}

func TestRateLimit() error {
	resetRedis(*redisAddr)
	IP := pseudoRandomIPV4Address()
	guardianConfig := guardianConfig{
		whitelist: []string{},
		blacklist: []string{},
		globalRateLimitConfig: `
version: "v0"
kind: GlobalRateLimit
name: GlobalRateLimit
description: GlobalRateLimit
globalRateLimitSpec:
 limit:
   count: 100
   duration: 1s
   enabled: false
`,
		globalSettingsConfig: `
version: "v0"
kind: GlobalSettings
name: GlobalSettings
description: GlobalSettings
globalSettingsSpec:
  reportOnly: false`,
		rateLimitConfig: `
version: "v0"
kind: RateLimit
name: "/foo/bar"
description: "/foo/bar"
rateLimitSpec:
  limit:
    count: 10
    duration: 1m
    enabled: true
  conditions:
    path: "/foo/bar"
---
version: "v0"
kind: RateLimit
name: "/foo/baz"
description: "/foo/baz"
rateLimitSpec:
  limit:
    count: 5
    duration: 1m
    enabled: false
  conditions:
    path: "/foo/baz"`,
	}

	applyGuardianConfig(*redisAddr, guardianConfig)
	dec := yaml.NewDecoder(strings.NewReader(guardianConfig.rateLimitConfig))

	for {
		config := guardian.RateLimitConfig{}
		err := dec.Decode(&config)
		if err == io.EOF {
			break
		} else if err != nil {
			return fmt.Errorf("error decoding yaml: %v", err)
		}
		for i := uint64(0); i < config.Spec.Limit.Count+5; i++ {
			if len(os.Getenv("SYNC")) == 0 {
				time.Sleep(defaultAsyncCounterTimeout) // helps prevents races due asynchronous rate limiting
			}

			res, err := GET(IP, config.Spec.Conditions.Path)
			if err != nil {
				return err
			}
			res.Body.Close()

			want := 200
			if i >= config.Spec.Limit.Count && config.Spec.Limit.Enabled {
				want = 429
			}

			if res.StatusCode != want {
				return fmt.Errorf("wanted %v, got %v, iteration %v", want, res.StatusCode, i)
			}
		}
	}
	return nil
}

func TestJails() error {
	resetRedis(*redisAddr)
	whitelistedIP := "192.168.1.1"

	guardianConfig := guardianConfig{
		whitelist: []string{whitelistedIP + "/32"},
		blacklist: []string{},
		globalRateLimitConfig: `
version: "v0"
kind: GlobalRateLimit
name: GlobalRateLimit
description: GlobalRateLimit
globalRateLimitSpec:
 limit:
   count: 5
   duration: 1m
   enabled: false
`,
		globalSettingsConfig: `
version: "v0"
kind: GlobalSettings
name: GlobalSettings
description: GlobalSettings
globalSettingsSpec:
  reportOnly: false`,
		jailConfig: `
version: "v0"
kind: Jail
name: "/foo/bar"
description: "/foo/bar"
jailSpec:
  limit:
    count: 10
    duration: 10s
    enabled: true
  conditions:
    path: "/foo/bar"
  banDuration: 30s # Keep this duration short as it's used in tests
---
version: "v0"
kind: Jail
name: "/foo/baz"
description: "/foo/baz"
jailSpec:
  limit:
    count: 5
    duration: 1m
    enabled: false
  conditions:
    path: "/foo/baz"
  banDuration: 30s # Keep this duration short as it's used in tests
`,
	}

	applyGuardianConfig(*redisAddr, guardianConfig)
	dec := yaml.NewDecoder(strings.NewReader(guardianConfig.jailConfig))

	// Assumes that any BanDuration in the Jail Config is greater than the time it takes
	// to execute this particular test.
	for {
		config := guardian.JailConfig{}
		err := dec.Decode(&config)
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("error decoding yaml: %v", err)
		}
		banned := false
		resetRedis(*redisAddr)
		applyGuardianConfig(*redisAddr, guardianConfig)
		for i := 0; uint64(i) <= config.Spec.Limit.Count; i++ {
			if os.Getenv("SYNC") != "" {
				time.Sleep(defaultAsyncCounterTimeout) // helps prevents races due asynchronous rate limiting
			}

			res, err := GET("192.168.1.43", config.Spec.Conditions.Path)
			if err != nil {
				return err
			}
			whitelistedRes, err := GET(whitelistedIP, config.Spec.Conditions.Path)
			if err != nil {
				return err
			}
			res.Body.Close()
			whitelistedRes.Body.Close()

			want := 200
			if (uint64(i) >= config.Spec.Limit.Count && config.Spec.Limit.Enabled) || banned {
				banned = true
				want = 429
			}

			if res.StatusCode != want {
				return fmt.Errorf("wanted %v, got %v, iteration %v, route: %v", want, res.StatusCode, i, config.Spec.Conditions.Path)
			}

			if whitelistedRes.StatusCode != 200 {
				return fmt.Errorf("whitelisted ip received unexpected status code: wanted %v, got %v, iteration %d, route: %v", 200, whitelistedRes.StatusCode, i, config.Spec.Conditions.Path)
			}
		}
		if config.Spec.Limit.Enabled {
			log.Printf("sleeping for banDuration: %v + 2 seconds to ensure the prisoner is removed", config.Spec.BanDuration)
			// ensure that we sleep for an additional confUpdateInterval so that the configuration is updated
			time.Sleep(config.Spec.BanDuration + (2 * time.Second))
			res, err := GET("192.168.1.43", config.Spec.Conditions.Path)
			if err != nil {
				return err
			}
			if res.StatusCode != 200 {
				return fmt.Errorf("prisoner was never removed, received unexpected status code: %d, %v", res.StatusCode, config.Spec.Jail)
			}
		}
	}
	return nil
}

func TestDeleteRateLimit() error {
	resetRedis(*redisAddr)

	config := guardianConfig{
		whitelist: []string{},
		blacklist: []string{},
		globalRateLimitConfig: `
version: "v0"
kind: GlobalRateLimit
name: GlobalRateLimit
description: GlobalRateLimit
globalRateLimitSpec:
 limit:
   count: 5
   duration: 1m
   enabled: false`,
		globalSettingsConfig: `
version: "v0"
kind: GlobalSettings
name: GlobalSettings
description: GlobalSettings
globalSettingsSpec:
  reportOnly: false`,
		rateLimitConfig: `
version: "v0"
kind: RateLimit
name: "/foo/bar"
description: "/foo/bar"
rateLimitSpec:
  limit:
    count: 10
    duration: 1m
    enabled: true
  conditions:
    path: "/foo/bar"
---
version: "v0"
kind: RateLimit
name: "/foo/baz"
description: "/foo/baz"
rateLimitSpec:
  limit:
    count: 5
    duration: 1m
    enabled: false
  conditions:
    path: "/foo/baz"`,
	}
	applyGuardianConfig(*redisAddr, config)
	delCmd := "delete"
	runGuardianCLI(*redisAddr, delCmd, "RateLimit", "/foo/bar")
	runGuardianCLI(*redisAddr, delCmd, "RateLimit", "/foo/baz")

	getCmd := "get"
	resStr, _ := runGuardianCLI(*redisAddr, getCmd, "RateLimit")

	if len(resStr) != 0 {
		return fmt.Errorf("get RateLimit returned non-empty output %v", resStr)
	}
	return nil
}

func TestDeleteJail() error {
	resetRedis(*redisAddr)

	config := guardianConfig{
		whitelist: []string{},
		blacklist: []string{},
		globalRateLimitConfig: `
version: "v0"
kind: GlobalRateLimit
name: GlobalRateLimit
description: GlobalRateLimit
globalRateLimitSpec:
 limit:
   count: 5
   duration: 1m
   enabled: false
`,
		globalSettingsConfig: `
version: "v0"
kind: GlobalSettings
name: GlobalSettings
description: GlobalSettings
globalSettingsSpec:
  reportOnly: false`,
		jailConfig: `
version: "v0"
kind: Jail
name: "/foo/bar"
description: "/foo/bar"
jailSpec:
  limit:
    count: 10
    duration: 10s
    enabled: true
  conditions:
    path: "/foo/bar"
  banDuration: 30s # Keep this duration short as it's used in tests
---
version: "v0"
kind: Jail
name: "/foo/baz"
description: "/foo/baz"
jailSpec:
  limit:
    count: 5
    duration: 1m
    enabled: false
  conditions:
    path: "/foo/baz"
  banDuration: 30s # Keep this duration short as it's used in tests`,
	}
	applyGuardianConfig(*redisAddr, config)
	delCmd := "delete"
	runGuardianCLI(*redisAddr, delCmd, "Jail", "/foo/bar")
	runGuardianCLI(*redisAddr, delCmd, "Jail", "/foo/baz")

	getCmd := "get"
	resStr, _ := runGuardianCLI(*redisAddr, getCmd, "Jail")

	if len(resStr) != 0 {
		return fmt.Errorf("get Jail returned non-empty output %v", resStr)
	}
	return nil
}

func GET(sourceIP string, path string) (*http.Response, error) {
	url := fmt.Sprintf("http://%v%v", *envoyAddr, path)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("error making request %v: %v", url, err)
	}
	req.Header.Add("X-Forwarded-For", fmt.Sprintf("%v, 10.0.0.123", sourceIP))

	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error running GET %v: %v", url, err)
	}

	return res, nil
}

type redisDBIndex struct {
	sync.Mutex
	Index int
}

var currentRedisDBIndex = redisDBIndex{
	Index: 0,
}

func resetRedis(redisAddr string) {
	currentRedisDBIndex.Lock()
	redisOpts := &redis.Options{
		Addr: redisAddr,
		DB:   currentRedisDBIndex.Index,
	}
	currentRedisDBIndex.Index++
	maxDBIndex := 15
	if currentRedisDBIndex.Index > maxDBIndex {
		currentRedisDBIndex.Index = 0
	}
	currentRedisDBIndex.Unlock()

	redis := redis.NewClient(redisOpts)
	redis.FlushAll()
}

type guardianConfig struct {
	whitelist             []string
	blacklist             []string
	globalRateLimitConfig string
	globalSettingsConfig  string
	rateLimitConfig       string
	jailConfig            string
	// Fields associated with deprecated CLI
	limitCountDeprecated               int
	limitDurationDeprecated            time.Duration
	limitEnabledDeprecated             bool
	reportOnlyDeprecated               bool
	routeRateLimitConfigPathDeprecated string
	jailConfigPathDeprecated           string
}

func applyGuardianConfig(redisAddr string, c guardianConfig) {
	if c.globalRateLimitConfig != "" {
		runGuardianCLIWithStdin(
			c.globalRateLimitConfig,
			redisAddr,
			"apply",
		)
	}

	if c.globalSettingsConfig != "" {
		runGuardianCLIWithStdin(
			c.globalSettingsConfig,
			redisAddr,
			"apply",
		)
	}

	clearXList(redisAddr, "blacklist")
	clearXList(redisAddr, "whitelist")

	if len(c.whitelist) > 0 {
		runGuardianCLI(redisAddr, "add-whitelist", strings.Join(c.whitelist, " "))
	}

	if len(c.blacklist) > 0 {
		runGuardianCLI(redisAddr, "add-blacklist", strings.Join(c.blacklist, " "))
	}

	if c.rateLimitConfig != "" {
		runGuardianCLIWithStdin(
			c.rateLimitConfig,
			redisAddr,
			"apply",
		)
	}

	if c.jailConfig != "" {
		runGuardianCLIWithStdin(
			c.jailConfig,
			redisAddr,
			"apply",
		)
	}

	time.Sleep(2 * time.Second)
}

func applyGuardianConfigDeprecated(redisAddr string, c guardianConfig) {
	runGuardianCLI(
		redisAddr,
		"set-limit",
		strconv.Itoa(c.limitCountDeprecated),
		c.limitDurationDeprecated.String(),
		strconv.FormatBool(c.limitEnabledDeprecated),
		"",
	)

	runGuardianCLI(redisAddr, "set-report-only", strconv.FormatBool(c.reportOnlyDeprecated))

	clearXList(redisAddr, "blacklist")
	clearXList(redisAddr, "whitelist")

	if len(c.whitelist) > 0 {
		runGuardianCLI(redisAddr, "add-whitelist", strings.Join(c.whitelist, " "))
	}

	if len(c.blacklist) > 0 {
		runGuardianCLI(redisAddr, "add-blacklist", strings.Join(c.blacklist, " "))
	}

	if len(c.routeRateLimitConfigPathDeprecated) > 0 {
		runGuardianCLI(redisAddr, "set-route-rate-limits", c.routeRateLimitConfigPathDeprecated)
	}

	if len(c.jailConfigPathDeprecated) > 0 {
		runGuardianCLI(redisAddr, "set-jails", c.jailConfigPathDeprecated)
	}

	time.Sleep(2 * time.Second)
}

func pseudoRandomIPV4Address() string {
	r := rand.New(rand.NewSource(time.Now().Unix()))
	buf := make([]byte, 4)
	n, err := r.Read(buf)
	if n != 4 || err != nil {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d.%d", buf[0], buf[1], buf[2], buf[3])
}

// listType one of blacklist whitelist
func clearXList(redisAddr string, listType string) {
	listCmd := fmt.Sprintf("get-%v", listType)
	currList, _ := runGuardianCLI(redisAddr, listCmd)
	currList = strings.Trim(currList, "\n")
	currList = strings.Trim(currList, " ")
	if len(currList) == 0 {
		return
	}

	currList = strings.Join(strings.Split(currList, "\n"), " ")
	clearCmd := fmt.Sprintf("remove-%v", listType)
	runGuardianCLI(redisAddr, clearCmd, currList)
}

func runGuardianCLI(redisAddr string, command string, args ...string) (out string, err string) {
	return runGuardianCLIWithStdin("", redisAddr, command, args...)
}

func runGuardianCLIWithStdin(stdin string, redisAddr string, command string, args ...string) (out string, err string) {
	cliPath := "/bin/guardian-cli"
	cmdArgs := append([]string{command, "-r", redisAddr}, args...)
	c := exec.Command(cliPath, cmdArgs...)
	if stdin != "" {
		c.Stdin = strings.NewReader(stdin)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	c.Run()
	return stdout.String(), stderr.String()
}
