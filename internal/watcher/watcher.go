package watcher

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/PatrickWalther/twitch-miner-go/internal/api"
	"github.com/PatrickWalther/twitch-miner-go/internal/config"
	"github.com/PatrickWalther/twitch-miner-go/internal/constants"
	"github.com/PatrickWalther/twitch-miner-go/internal/models"
)

type MinuteWatcher struct {
	client     *api.TwitchClient
	streamers  []*models.Streamer
	priorities []config.Priority
	settings   config.RateLimitSettings

	ctx    context.Context
	cancel context.CancelFunc

	httpClient *http.Client

	mu sync.RWMutex
}

func NewMinuteWatcher(
	client *api.TwitchClient,
	streamers []*models.Streamer,
	priorities []config.Priority,
	settings config.RateLimitSettings,
) *MinuteWatcher {
	return &MinuteWatcher{
		client:     client,
		streamers:  streamers,
		priorities: priorities,
		settings:   settings,
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

func (w *MinuteWatcher) Start(ctx context.Context) {
	w.mu.Lock()
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.mu.Unlock()

	go w.loop()
}

func (w *MinuteWatcher) Stop() {
	w.mu.Lock()
	if w.cancel != nil {
		w.cancel()
	}
	w.mu.Unlock()
}

func (w *MinuteWatcher) UpdateSettings(priorities []config.Priority, settings config.RateLimitSettings) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.priorities = priorities
	w.settings = settings
}

func (w *MinuteWatcher) randomizedDelay(base time.Duration) time.Duration {
	jitter := (rand.Float64() - 0.5) * 0.4
	return time.Duration(float64(base) * (1.0 + jitter))
}

func (w *MinuteWatcher) loop() {
	for {
		select {
		case <-w.ctx.Done():
			return
		default:
		}

		w.processWatching()

		interval := time.Duration(w.settings.MinuteWatchedInterval) * time.Second
		select {
		case <-w.ctx.Done():
			return
		case <-time.After(w.randomizedDelay(interval)):
		}
	}
}

func (w *MinuteWatcher) processWatching() {
	onlineStreamers := w.getOnlineStreamers()
	if len(onlineStreamers) == 0 {
		return
	}

	for _, idx := range onlineStreamers {
		if w.streamers[idx].Stream.UpdateElapsed() > 10*time.Minute {
			w.client.CheckStreamerOnline(w.streamers[idx])
		}
	}

	watching := w.selectStreamersToWatch(onlineStreamers)
	if len(watching) == 0 {
		return
	}

	var watchingNames []string
	for _, idx := range watching {
		watchingNames = append(watchingNames, w.streamers[idx].Username)
	}
	slog.Debug("Watching streams", "count", len(watching), "max", constants.MaxSimultaneousStreams, "streamers", watchingNames)

	sleepBetween := time.Duration(w.settings.MinuteWatchedInterval) * time.Second / time.Duration(len(watching))

	for _, idx := range watching {
		streamer := w.streamers[idx]

		if err := w.sendMinuteWatched(streamer); err != nil {
			slog.Debug("Failed to send minute watched", "streamer", streamer.Username, "error", err)
		} else {
			slog.Debug("Sent minute watched", "streamer", streamer.Username, "minutesWatched", streamer.Stream.MinuteWatched)
			streamer.Stream.UpdateMinuteWatched()
		}

		select {
		case <-w.ctx.Done():
			return
		case <-time.After(w.randomizedDelay(sleepBetween)):
		}
	}
}

func (w *MinuteWatcher) getOnlineStreamers() []int {
	var online []int
	for i, s := range w.streamers {
		if s.GetIsOnline() {
			if s.GetOnlineAt().IsZero() || time.Since(s.GetOnlineAt()) > 30*time.Second {
				online = append(online, i)
			}
		}
	}
	return online
}

func (w *MinuteWatcher) selectStreamersToWatch(onlineIndexes []int) []int {
	watching := make(map[int]bool)

	remainingSlots := func() int {
		return constants.MaxSimultaneousStreams - len(watching)
	}

	for _, priority := range w.priorities {
		if remainingSlots() <= 0 {
			break
		}

		switch priority {
		case config.PriorityOrder:
			for _, idx := range onlineIndexes {
				if !watching[idx] {
					watching[idx] = true
					if remainingSlots() <= 0 {
						break
					}
				}
			}

		case config.PriorityPointsAscending, config.PriorityPointsDescending:
			type indexedPoints struct {
				index  int
				points int
			}
			items := make([]indexedPoints, 0, len(onlineIndexes))
			for _, idx := range onlineIndexes {
				items = append(items, indexedPoints{index: idx, points: w.streamers[idx].GetChannelPoints()})
			}
			sort.Slice(items, func(i, j int) bool {
				if priority == config.PriorityPointsAscending {
					return items[i].points < items[j].points
				}
				return items[i].points > items[j].points
			})
			for _, item := range items {
				if !watching[item.index] {
					watching[item.index] = true
					if remainingSlots() <= 0 {
						break
					}
				}
			}

		case config.PriorityStreak:
			for _, idx := range onlineIndexes {
				s := w.streamers[idx]
				if s.Settings.WatchStreak &&
					s.Stream.WatchStreakMissing &&
					(s.GetOfflineAt().IsZero() || time.Since(s.GetOfflineAt()) > 30*time.Minute) &&
					s.Stream.MinuteWatched < 7 {
					if !watching[idx] {
						watching[idx] = true
						if remainingSlots() <= 0 {
							break
						}
					}
				}
			}

		case config.PriorityDrops:
			for _, idx := range onlineIndexes {
				if w.streamers[idx].DropsCondition() {
					if !watching[idx] {
						watching[idx] = true
						if remainingSlots() <= 0 {
							break
						}
					}
				}
			}

		case config.PrioritySubscribed:
			type indexedMultiplier struct {
				index      int
				multiplier float64
			}
			var items []indexedMultiplier
			for _, idx := range onlineIndexes {
				if w.streamers[idx].ViewerHasPointsMultiplier() {
					items = append(items, indexedMultiplier{
						index:      idx,
						multiplier: w.streamers[idx].TotalPointsMultiplier(),
					})
				}
			}
			sort.Slice(items, func(i, j int) bool {
				return items[i].multiplier > items[j].multiplier
			})
			for _, item := range items {
				if !watching[item.index] {
					watching[item.index] = true
					if remainingSlots() <= 0 {
						break
					}
				}
			}
		}
	}

	result := make([]int, 0, len(watching))
	for idx := range watching {
		result = append(result, idx)
	}
	return result
}

func (w *MinuteWatcher) sendMinuteWatched(streamer *models.Streamer) error {
	sig, token, err := w.client.GetPlaybackAccessToken(streamer.Username)
	if err != nil {
		return fmt.Errorf("failed to get playback token: %w", err)
	}

	if err := w.simulateWatching(streamer.Username, sig, token); err != nil {
		slog.Debug("Failed to simulate watching", "streamer", streamer.Username, "error", err)
	}

	if streamer.Stream.SpadeURL == "" {
		return fmt.Errorf("no spade URL")
	}

	payload, err := streamer.Stream.EncodePayload()
	if err != nil {
		return fmt.Errorf("failed to encode payload: %w", err)
	}

	req, err := http.NewRequest("POST", streamer.Stream.SpadeURL, strings.NewReader("data="+payload))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", constants.TVUserAgent)

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

func (w *MinuteWatcher) simulateWatching(channel, sig, token string) error {
	playlistURL := fmt.Sprintf("%s/api/channel/hls/%s.m3u8", constants.UsherURL, channel)

	params := url.Values{
		"sig":   {sig},
		"token": {token},
	}

	resp, err := w.httpClient.Get(playlistURL + "?" + params.Encode())
	if err != nil {
		return fmt.Errorf("failed to get playlist: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("playlist request failed with status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read playlist: %w", err)
	}

	lines := strings.Split(string(body), "\n")
	var lowestQualityURL string
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "http") {
			lowestQualityURL = line
			break
		}
	}

	if lowestQualityURL == "" {
		return fmt.Errorf("no stream URL found in playlist")
	}

	streamListResp, err := w.httpClient.Get(lowestQualityURL)
	if err != nil {
		return fmt.Errorf("failed to get stream list: %w", err)
	}
	defer func() { _ = streamListResp.Body.Close() }()

	if streamListResp.StatusCode != http.StatusOK {
		return fmt.Errorf("stream list request failed with status %d", streamListResp.StatusCode)
	}

	streamListBody, err := io.ReadAll(streamListResp.Body)
	if err != nil {
		return fmt.Errorf("failed to read stream list: %w", err)
	}

	streamLines := strings.Split(string(streamListBody), "\n")
	var segmentURL string
	for i := len(streamLines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(streamLines[i])
		if strings.HasPrefix(line, "http") {
			segmentURL = line
			break
		}
	}

	if segmentURL == "" {
		return fmt.Errorf("no segment URL found")
	}

	req, err := http.NewRequest("HEAD", segmentURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create HEAD request: %w", err)
	}
	req.Header.Set("User-Agent", constants.TVUserAgent)

	headResp, err := w.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("HEAD request failed: %w", err)
	}
	defer func() { _ = headResp.Body.Close() }()

	if headResp.StatusCode != http.StatusOK {
		return fmt.Errorf("HEAD request returned status %d", headResp.StatusCode)
	}

	return nil
}
