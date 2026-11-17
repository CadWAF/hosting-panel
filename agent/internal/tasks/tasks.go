// Package tasks runs long operations asynchronously with a persisted, live log.
package tasks

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"hostingpanel/agent/internal/store"
)

type Manager struct {
	st *store.Store
	mu sync.Mutex
	wg sync.WaitGroup
}

func New(st *store.Store) *Manager { return &Manager{st: st} }

// Log is the writer a task body logs to; every write is persisted immediately.
type Log struct {
	m   *Manager
	id  string
	mu  sync.Mutex
	buf strings.Builder
}

func (l *Log) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buf.Len() < 256*1024 {
		l.buf.Write(p)
	}
	_ = l.m.st.UpdateTask(l.id, "running", l.buf.String(), nil)
	return len(p), nil
}

func (l *Log) Printf(f string, a ...any) { fmt.Fprintf(l, f+"\n", a...) }

var _ io.Writer = (*Log)(nil)

// Start runs fn in the background and returns the task immediately.
func (m *Manager) Start(kind, title string, fn func(l *Log) error) (store.Task, error) {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	t := store.Task{ID: hex.EncodeToString(b), Kind: kind, Title: title, Status: "running", CreatedAt: time.Now().UTC()}
	if err := m.st.InsertTask(t); err != nil {
		return t, err
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		l := &Log{m: m, id: t.ID}
		err := safe(fn, l)
		status := "done"
		if err != nil {
			status = "failed"
			l.Printf("ERROR: %v", err)
		}
		now := time.Now().UTC()
		l.mu.Lock()
		_ = m.st.UpdateTask(t.ID, status, l.buf.String(), &now)
		l.mu.Unlock()
	}()
	return t, nil
}

func safe(fn func(*Log) error, l *Log) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New(fmt.Sprint("panic: ", r))
		}
	}()
	return fn(l)
}

// Wait blocks until all running tasks finish (tests, graceful shutdown).
func (m *Manager) Wait() { m.wg.Wait() }

func (m *Manager) Get(id string) (store.Task, error) { return m.st.GetTask(id) }
func (m *Manager) List() ([]store.Task, error)       { return m.st.ListTasks(50) }
