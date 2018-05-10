package guardian

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DataDog/datadog-go/statsd"
)

func TestDatadogReportSetsDefaultTags(t *testing.T) {
	writer := &testStatsdWriter{}
	client, err := statsd.NewWithWriter(writer)
	if err != nil {
		t.Fatalf("got err: %v", err)
	}

	defaultTags := []string{"default1:tag1", "default2:tag2"}
	reporter := NewDataDogReporter(client, defaultTags, TestingLogger)

	stop := make(chan struct{})
	defer close(stop)

	go func() {
		reporter.Run(stop)
	}()

	req := Request{}

	reporter.Duration(req, false, false, time.Second)
	reporter.HandledWhitelist(req, true, false, time.Second)
	reporter.HandledRatelimit(req, true, false, time.Second)
	reporter.RedisCounterIncr(time.Second, false)
	reporter.RedisCounterPruned(time.Second, 100, 20)
	reporter.CurrentLimit(Limit{})
	reporter.CurrentWhitelist([]net.IPNet{})
	reporter.CurrentReportOnlyMode(false)

	time.Sleep(time.Second) // wait for all the go funcs to run

	if len(writer.received) == 0 {
		t.Fatalf("expected: %v, received: %v", "> 0", len(writer.received))
	}

	for _, stat := range writer.received {
		if !contains(stat.tags, defaultTags) {
			t.Fatalf("expected contains: %v, received: %v", defaultTags, stat.tags)
		}
	}
}

func contains(x []tag, y []string) bool {
	lookup := make(map[string]bool)
	for _, s := range x {
		lookup[string(s)] = true
	}

	for _, s := range y {
		if lookup[s] != true {
			return false
		}
	}

	return true
}

type tag string

func (t tag) Name() string {
	return strings.Split(string(t), ":")[0]
}

func (t tag) Value() string {
	return strings.Split(string(t), ":")[1]
}

type stat struct {
	name       string
	value      string
	statType   string
	sampleRate float64
	tags       []tag
}

type testStatsdWriter struct {
	received []stat
}

func (ts testStatsdWriter) parseStat(str string) (stat, error) {
	stat := stat{}
	comps := strings.Split(str, "|")
	if len(comps) < 2 {
		return stat, fmt.Errorf("invalid stat")
	}

	metric := strings.Split(comps[0], ":")

	stat.name = metric[0]
	stat.value = metric[1]
	stat.statType = comps[1]
	stat.sampleRate = 1.0

	if len(comps) > 2 {
		for i := 2; i < len(comps); i++ {
			item := comps[i]
			if strings.HasPrefix(item, "#") {
				stat.tags = ts.parseTags(strings.TrimLeft(item, "#"))
			}
			if strings.HasPrefix(item, "@") {
				floatStr := strings.TrimLeft(item, "@")
				stat.sampleRate, _ = strconv.ParseFloat(floatStr, 64)
			}
		}
	}

	return stat, nil
}

func (ts *testStatsdWriter) parseTags(str string) []tag {
	tags := []tag{}
	comps := strings.Split(str, ",")
	for _, tStr := range comps {
		tags = append(tags, tag(tStr))
	}

	return tags
}

func (ts *testStatsdWriter) Write(data []byte) (n int, err error) {
	str := string(data)
	for _, line := range strings.Split(str, "\n") {
		stat, err := ts.parseStat(line)
		if err != nil {
			continue
		}

		ts.received = append(ts.received, stat)
	}

	return len(data), nil
}

func (ts *testStatsdWriter) SetWriteTimeout(time.Duration) error {
	return nil
}

func (ts *testStatsdWriter) Close() error {
	return nil
}
