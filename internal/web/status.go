package web

import (
	"sync"
)

type MinerStatus string

const (
	StatusInitializing     MinerStatus = "initializing"
	StatusAuthRequired     MinerStatus = "auth_required"
	StatusAuthWaiting      MinerStatus = "auth_waiting"
	StatusLoadingStreamers MinerStatus = "loading_streamers"
	StatusRunning          MinerStatus = "running"
	StatusError            MinerStatus = "error"
)

type AuthInfo struct {
	VerificationURI string `json:"verificationUri,omitempty"`
	UserCode        string `json:"userCode,omitempty"`
	ExpiresIn       int    `json:"expiresIn,omitempty"`
}

type StatusInfo struct {
	Status       MinerStatus `json:"status"`
	Message      string      `json:"message,omitempty"`
	Auth         *AuthInfo   `json:"auth,omitempty"`
	StreamerInfo string      `json:"streamerInfo,omitempty"`
}

type StatusBroadcaster struct {
	status    StatusInfo
	listeners []chan StatusInfo
	mu        sync.RWMutex
}

func NewStatusBroadcaster() *StatusBroadcaster {
	return &StatusBroadcaster{
		status: StatusInfo{
			Status:  StatusInitializing,
			Message: "Starting up...",
		},
	}
}

func (b *StatusBroadcaster) GetStatus() StatusInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.status
}

func (b *StatusBroadcaster) SetStatus(status MinerStatus, message string) {
	b.mu.Lock()
	b.status = StatusInfo{
		Status:  status,
		Message: message,
	}
	current := b.status
	b.mu.Unlock()

	b.broadcast(current)
}

func (b *StatusBroadcaster) SetAuthRequired(verificationURI, userCode string, expiresIn int) {
	b.mu.Lock()
	b.status = StatusInfo{
		Status:  StatusAuthRequired,
		Message: "Please authorize with Twitch",
		Auth: &AuthInfo{
			VerificationURI: verificationURI,
			UserCode:        userCode,
			ExpiresIn:       expiresIn,
		},
	}
	current := b.status
	b.mu.Unlock()

	b.broadcast(current)
}

func (b *StatusBroadcaster) SetStreamerProgress(current, total int, name string) {
	b.mu.Lock()
	b.status = StatusInfo{
		Status:       StatusLoadingStreamers,
		Message:      "Loading streamers...",
		StreamerInfo: name,
	}
	current2 := b.status
	b.mu.Unlock()

	b.broadcast(current2)
}

func (b *StatusBroadcaster) Subscribe() chan StatusInfo {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan StatusInfo, 10)
	b.listeners = append(b.listeners, ch)
	ch <- b.status
	return ch
}

func (b *StatusBroadcaster) Unsubscribe(ch chan StatusInfo) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i, listener := range b.listeners {
		if listener == ch {
			b.listeners = append(b.listeners[:i], b.listeners[i+1:]...)
			close(ch)
			return
		}
	}
}

func (b *StatusBroadcaster) broadcast(status StatusInfo) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.listeners {
		select {
		case ch <- status:
		default:
		}
	}
}
