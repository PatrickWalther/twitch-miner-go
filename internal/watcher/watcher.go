package watcher

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/patrickdappollonio/twitch-miner/internal/api"
	"github.com/patrickdappollonio/twitch-miner/internal/config"
	"github.com/patrickdappollonio/twitch-miner/internal/constants"
	"github.com/patrickdappollonio/twitch-miner/internal/models"
)

type MinuteWatcher struct {
	client     *api.TwitchClient
	streamers  []*models.Streamer
	priorities []config.Priority
	settings   config.RateLimitSettings
	running    bool
	stopChan   chan struct{}

	httpClient *http.Client
	m3u8Regex  *regexp.Regexp

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
		stopChan:   make(chan struct{}),
		httpClient: &http.Client{Timeout: 20 * time.Second},
		m3u8Regex:  regexp.MustCompile(`(?m)^[^#].*\.m3u8$`),
	}
}

func (w *MinuteWatcher) Start() {
	w.mu.Lock()
	w.running = true
	w.mu.Unlock()

	go w.loop()
}

func (w *MinuteWatcher) Stop() {
	w.mu.Lock()
	w.running = false
	w.mu.Unlock()

	close(w.stopChan)
}

func (w *MinuteWatcher) IsRunning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.running
}

func (w *MinuteWatcher) loop() {
	interval := time.Duration(w.settings.MinuteWatchedInterval) * time.Second

	for {
		select {
		case <-w.stopChan:
			return
		default:
		}

		w.processWatching()

		select {
		case <-w.stopChan:
			return
		case <-time.After(interval):
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

	sleepBetween := time.Duration(w.settings.MinuteWatchedInterval) * time.Second / time.Duration(len(watching))

	for _, idx := range watching {
		streamer := w.streamers[idx]

		if err := w.sendMinuteWatched(streamer); err != nil {
			slog.Debug("Failed to send minute watched", "streamer", streamer.Username, "error", err)
		} else {
			streamer.Stream.UpdateMinuteWatched()
		}

		select {
		case <-w.stopChan:
			return
		case <-time.After(sleepBetween):
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

	streamURL := w.getLowestQualityStreamURL(streamer.Username, sig, token)
	if streamURL != "" {
		resp, err := w.httpClient.Get(streamURL)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
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

func (w *MinuteWatcher) getLowestQualityStreamURL(channel, sig, token string) string {
	playlistURL := fmt.Sprintf("%s/api/channel/hls/%s.m3u8", constants.UsherURL, channel)

	params := url.Values{
		"sig":          {sig},
		"token":        {token},
		"player_type":  {"site"},
		"allow_source": {"true"},
	}

	resp, err := w.httpClient.Get(playlistURL + "?" + params.Encode())
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	matches := w.m3u8Regex.FindAllString(string(body), -1)
	if len(matches) == 0 {
		return ""
	}

	return matches[len(matches)-1]
}
