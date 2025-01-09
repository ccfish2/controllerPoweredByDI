package identitygc

import (
	"time"

	"github.com/ccfish2/infra/pkg/lock"
)

type heartbeatStore struct {
	mutex        lock.RWMutex
	lastLifeSign map[string]time.Time
	firstRun     time.Time
	timeout      time.Duration
}

func newheartbeatStore(timeout time.Duration) *heartbeatStore {
	i := &heartbeatStore{
		timeout:      timeout,
		lastLifeSign: map[string]time.Time{},
		firstRun:     time.Now(),
	}
	return i
}
