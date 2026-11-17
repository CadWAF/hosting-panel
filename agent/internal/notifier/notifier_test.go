package notifier

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeSender struct{ msgs []string }

func (f *fakeSender) Name() string { return "fake" }
func (f *fakeSender) Send(t, b string) error {
	f.msgs = append(f.msgs, t)
	return nil
}

type badSender struct{}

func (badSender) Name() string           { return "bad" }
func (badSender) Send(_, _ string) error { return errors.New("boom") }

func TestDownAfterThresholdThenRecoveryOnce(t *testing.T) {
	healthy := false
	f := &fakeSender{}
	m := NewMonitor(Config{FailThreshold: 2, Checks: []Check{{Name: "panel", Kind: "http", Target: "x"}}},
		[]Sender{f}, func(context.Context, Check) error {
			if healthy {
				return nil
			}
			return errors.New("refused")
		})
	ctx := context.Background()
	m.Tick(ctx)
	if len(f.msgs) != 0 {
		t.Fatal("one failure must not alert")
	}
	m.Tick(ctx)
	m.Tick(ctx)
	if len(f.msgs) != 1 || !strings.Contains(f.msgs[0], "panel is down") {
		t.Fatalf("want exactly one down alert, got %v", f.msgs)
	}
	healthy = true
	m.Tick(ctx)
	m.Tick(ctx)
	if len(f.msgs) != 2 || !strings.Contains(f.msgs[1], "back") {
		t.Fatalf("want one recovery, got %v", f.msgs)
	}
}

func TestOneBrokenChannelDoesNotBlockOthers(t *testing.T) {
	f := &fakeSender{}
	m := NewMonitor(Config{}, []Sender{badSender{}, f}, nil)
	m.Notify("hi", "x")
	if len(f.msgs) != 1 {
		t.Fatal("second channel must still receive")
	}
}

func TestEventsEndpointForwardsToChannels(t *testing.T) {
	f := &fakeSender{}
	m := NewMonitor(Config{}, []Sender{f}, nil)
	rr := httptest.NewRecorder()
	m.Handler().ServeHTTP(rr, httptest.NewRequest("POST", "/v1/events", strings.NewReader(`{"level":"error","title":"Backup failed","message":"x"}`)))
	if rr.Code != 202 || len(f.msgs) != 1 || !strings.Contains(f.msgs[0], "Backup failed") {
		t.Fatalf("code %d msgs %v", rr.Code, f.msgs)
	}
	rr = httptest.NewRecorder()
	m.Handler().ServeHTTP(rr, httptest.NewRequest("POST", "/v1/events", strings.NewReader(`{}`)))
	if rr.Code != 422 {
		t.Fatalf("empty event must be rejected, got %d", rr.Code)
	}
}

func TestDiskAlertOnCrossing(t *testing.T) {
	f := &fakeSender{}
	m := NewMonitor(Config{DiskPercent: 90}, []Sender{f}, nil)
	use := 50.0
	m.DiskUsed = func() (float64, error) { return use, nil }
	m.Tick(context.Background())
	use = 95
	m.Tick(context.Background())
	m.Tick(context.Background())
	if len(f.msgs) != 1 || !strings.Contains(f.msgs[0], "Disk") {
		t.Fatalf("%v", f.msgs)
	}
}
