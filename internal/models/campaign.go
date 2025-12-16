package models

import "time"

type CampaignStatus string

const (
	CampaignActive  CampaignStatus = "ACTIVE"
	CampaignExpired CampaignStatus = "EXPIRED"
)

type Campaign struct {
	ID          string
	Name        string
	Game        *Game
	Status      CampaignStatus
	StartAt     time.Time
	EndAt       time.Time
	Channels    []string
	InInventory bool
	Drops       []*Drop
	DateMatch   bool
}

func NewCampaignFromGQL(data map[string]interface{}) *Campaign {
	c := &Campaign{
		Drops: make([]*Drop, 0),
	}

	if id, ok := data["id"].(string); ok {
		c.ID = id
	}
	if name, ok := data["name"].(string); ok {
		c.Name = name
	}
	if status, ok := data["status"].(string); ok {
		c.Status = CampaignStatus(status)
	}

	if gameData, ok := data["game"].(map[string]interface{}); ok {
		c.Game = &Game{}
		if id, ok := gameData["id"].(string); ok {
			c.Game.ID = id
		}
		if name, ok := gameData["name"].(string); ok {
			c.Game.Name = name
		}
		if displayName, ok := gameData["displayName"].(string); ok {
			c.Game.DisplayName = displayName
		}
	}

	if startAt, ok := data["startAt"].(string); ok {
		if t, err := time.Parse(time.RFC3339, startAt); err == nil {
			c.StartAt = t
		}
	}
	if endAt, ok := data["endAt"].(string); ok {
		if t, err := time.Parse(time.RFC3339, endAt); err == nil {
			c.EndAt = t
		}
	}

	now := time.Now()
	c.DateMatch = c.StartAt.Before(now) && c.EndAt.After(now)

	if allow, ok := data["allow"].(map[string]interface{}); ok {
		if channels, ok := allow["channels"].([]interface{}); ok && channels != nil {
			for _, ch := range channels {
				if chMap, ok := ch.(map[string]interface{}); ok {
					if id, ok := chMap["id"].(string); ok {
						c.Channels = append(c.Channels, id)
					}
				}
			}
		}
	}

	if drops, ok := data["timeBasedDrops"].([]interface{}); ok {
		for _, d := range drops {
			if dropData, ok := d.(map[string]interface{}); ok {
				c.Drops = append(c.Drops, NewDropFromGQL(dropData))
			}
		}
	}

	return c
}

func (c *Campaign) ClearClaimedDrops() {
	validDrops := make([]*Drop, 0)
	for _, drop := range c.Drops {
		if drop.DateTimeMatch() && !drop.IsClaimed {
			validDrops = append(validDrops, drop)
		}
	}
	c.Drops = validDrops
}

func (c *Campaign) SyncDrops(inventoryDrops []interface{}, claimFunc func(*Drop) bool) {
	for _, invDrop := range inventoryDrops {
		dropData, ok := invDrop.(map[string]interface{})
		if !ok {
			continue
		}
		dropID, ok := dropData["id"].(string)
		if !ok {
			continue
		}

		for _, drop := range c.Drops {
			if drop.ID == dropID {
				if selfData, ok := dropData["self"].(map[string]interface{}); ok {
					drop.Update(selfData)
				}
				if drop.IsClaimable && claimFunc != nil {
					drop.IsClaimed = claimFunc(drop)
				}
				break
			}
		}
	}
}
