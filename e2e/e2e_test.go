package e2e

import (
	"flag"
	"fmt"
	"go/build"
	"net/http"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redis"
)

var redisAddr = flag.String("redis-addr", "localhost:6379", "redis address")
var envoyAddr = flag.String("envoy-addr", "localhost:8080", "envoy address")

func TestRateLimit(t *testing.T) {
	config := guardianConfig{
		whitelist:     []string{},
		blacklist:     []string{},
		limitCount:    5,
		limitDuration: time.Minute,
		limitEnabled:  true,
		reportOnly:    false,
	}
	applyGuardianConfig(t, *redisAddr, config)

	count200 := 0
	count429 := 0
	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond) // helps prevents races due asynchronous rate limiting
		res := GET(t, "192.168.1.234", "/")
		res.Body.Close()

		if res.StatusCode == 200 {
			count200++
		}

		if res.StatusCode == 429 {
			count429++
		}

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
	whitelist     []string
	blacklist     []string
	limitCount    int
	limitDuration time.Duration
	limitEnabled  bool
	reportOnly    bool
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
