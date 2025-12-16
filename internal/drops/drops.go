package drops

import (
	"log/slog"
	"sync"
	"time"

	"github.com/patrickdappollonio/twitch-miner/internal/api"
	"github.com/patrickdappollonio/twitch-miner/internal/config"
	"github.com/patrickdappollonio/twitch-miner/internal/constants"
	"github.com/patrickdappollonio/twitch-miner/internal/models"
)

type DropsTracker struct {
	client    *api.TwitchClient
	streamers []*models.Streamer
	settings  config.RateLimitSettings

	campaigns []*models.Campaign
	running   bool
	stopChan  chan struct{}

	mu sync.RWMutex
}

func NewDropsTracker(
	client *api.TwitchClient,
	streamers []*models.Streamer,
	settings config.RateLimitSettings,
) *DropsTracker {
	return &DropsTracker{
		client:    client,
		streamers: streamers,
		settings:  settings,
		stopChan:  make(chan struct{}),
	}
}

func (d *DropsTracker) Start() {
	d.mu.Lock()
	d.running = true
	d.mu.Unlock()

	go d.loop()
}

func (d *DropsTracker) Stop() {
	d.mu.Lock()
	d.running = false
	d.mu.Unlock()

	close(d.stopChan)
}

func (d *DropsTracker) loop() {
	syncInterval := time.Duration(d.settings.CampaignSyncInterval) * time.Minute

	d.syncCampaigns()

	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopChan:
			return
		case <-ticker.C:
			d.syncCampaigns()
		}
	}
}

func (d *DropsTracker) syncCampaigns() {
	d.claimAllDropsFromInventory()

	campaigns, err := d.getActiveCampaigns()
	if err != nil {
		slog.Error("Failed to get campaigns", "error", err)
		return
	}

	campaigns = d.syncWithInventory(campaigns)

	d.mu.Lock()
	d.campaigns = campaigns
	d.mu.Unlock()

	d.updateStreamerCampaigns()
}

func (d *DropsTracker) getActiveCampaigns() ([]*models.Campaign, error) {
	dashboardCampaigns, err := d.getDropsDashboard("ACTIVE")
	if err != nil {
		return nil, err
	}

	var campaigns []*models.Campaign
	for _, c := range dashboardCampaigns {
		campaign := models.NewCampaignFromGQL(c)
		if campaign.DateMatch {
			campaign.ClearClaimedDrops()
			if len(campaign.Drops) > 0 {
				campaigns = append(campaigns, campaign)
			}
		}
	}

	return campaigns, nil
}

func (d *DropsTracker) getDropsDashboard(status string) ([]map[string]interface{}, error) {
	resp, err := d.client.PostGQL(constants.ViewerDropsDashboard)
	if err != nil {
		return nil, err
	}

	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		return nil, nil
	}

	currentUser, ok := data["currentUser"].(map[string]interface{})
	if !ok || currentUser == nil {
		return nil, nil
	}

	campaignsData, ok := currentUser["dropCampaigns"].([]interface{})
	if !ok || campaignsData == nil {
		return nil, nil
	}

	var result []map[string]interface{}
	for _, c := range campaignsData {
		campaign, ok := c.(map[string]interface{})
		if !ok {
			continue
		}

		if status != "" {
			if s, ok := campaign["status"].(string); ok && s != status {
				continue
			}
		}

		result = append(result, campaign)
	}

	return result, nil
}

func (d *DropsTracker) getInventory() (map[string]interface{}, error) {
	resp, err := d.client.PostGQL(constants.Inventory)
	if err != nil {
		return nil, err
	}

	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		return nil, nil
	}

	currentUser, ok := data["currentUser"].(map[string]interface{})
	if !ok || currentUser == nil {
		return nil, nil
	}

	inventory, ok := currentUser["inventory"].(map[string]interface{})
	if !ok {
		return nil, nil
	}

	return inventory, nil
}

func (d *DropsTracker) syncWithInventory(campaigns []*models.Campaign) []*models.Campaign {
	inventory, err := d.getInventory()
	if err != nil || inventory == nil {
		return campaigns
	}

	inProgress, ok := inventory["dropCampaignsInProgress"].([]interface{})
	if !ok || inProgress == nil {
		return campaigns
	}

	for _, campaign := range campaigns {
		campaign.ClearClaimedDrops()

		for _, prog := range inProgress {
			progData, ok := prog.(map[string]interface{})
			if !ok {
				continue
			}

			progID, ok := progData["id"].(string)
			if !ok || progID != campaign.ID {
				continue
			}

			campaign.InInventory = true

			if drops, ok := progData["timeBasedDrops"].([]interface{}); ok {
				campaign.SyncDrops(drops, func(drop *models.Drop) bool {
					claimed, err := d.client.ClaimDrop(drop)
					if err != nil {
						slog.Error("Failed to claim drop", "drop", drop.Name, "error", err)
						return false
					}
					return claimed
				})
			}

			campaign.ClearClaimedDrops()
			break
		}
	}

	return campaigns
}

func (d *DropsTracker) claimAllDropsFromInventory() {
	inventory, err := d.getInventory()
	if err != nil || inventory == nil {
		return
	}

	inProgress, ok := inventory["dropCampaignsInProgress"].([]interface{})
	if !ok || inProgress == nil {
		return
	}

	for _, campaign := range inProgress {
		campaignData, ok := campaign.(map[string]interface{})
		if !ok {
			continue
		}

		drops, ok := campaignData["timeBasedDrops"].([]interface{})
		if !ok || drops == nil {
			continue
		}

		for _, dropData := range drops {
			dropMap, ok := dropData.(map[string]interface{})
			if !ok {
				continue
			}

			drop := models.NewDropFromGQL(dropMap)
			if selfData, ok := dropMap["self"].(map[string]interface{}); ok {
				drop.Update(selfData)
			}

			if drop.IsClaimable {
				if claimed, err := d.client.ClaimDrop(drop); err != nil {
					slog.Error("Failed to claim drop", "drop", drop.Name, "error", err)
				} else if claimed {
					slog.Info("Claimed drop", "drop", drop.Name)
				}
				time.Sleep(5 * time.Second)
			}
		}
	}
}

func (d *DropsTracker) updateStreamerCampaigns() {
	d.mu.RLock()
	campaigns := d.campaigns
	d.mu.RUnlock()

	for _, streamer := range d.streamers {
		if !streamer.DropsCondition() {
			continue
		}

		var streamerCampaigns []*models.Campaign
		for _, campaign := range campaigns {
			if len(campaign.Drops) == 0 {
				continue
			}

			if campaign.Game == nil || streamer.Stream.GameID() == "" {
				continue
			}

			if campaign.Game.ID != streamer.Stream.GameID() {
				continue
			}

			hasID := false
			for _, id := range streamer.Stream.CampaignIDs {
				if id == campaign.ID {
					hasID = true
					break
				}
			}

			if hasID {
				streamerCampaigns = append(streamerCampaigns, campaign)
			}
		}

		streamer.Stream.Campaigns = streamerCampaigns
	}
}
