package pubsub

import (
	"encoding/json"
	"time"
)

type WSMessage struct {
	Type  string  `json:"type"`
	Nonce string  `json:"nonce,omitempty"`
	Data  *WSData `json:"data,omitempty"`
	Error string  `json:"error,omitempty"`
}

type WSData struct {
	Topics    []string `json:"topics,omitempty"`
	AuthToken string   `json:"auth_token,omitempty"`
	Topic     string   `json:"topic,omitempty"`
	Message   string   `json:"message,omitempty"`
}

type PubSubMessage struct {
	Topic     Topic
	Type      string
	Data      map[string]interface{}
	Message   map[string]interface{}
	Timestamp time.Time
	ChannelID string
}

func ParsePubSubMessage(data *WSData) (*PubSubMessage, error) {
	topic, err := ParseTopic(data.Topic)
	if err != nil {
		return nil, err
	}

	var message map[string]interface{}
	if err := json.Unmarshal([]byte(data.Message), &message); err != nil {
		return nil, err
	}

	msg := &PubSubMessage{
		Topic:     topic,
		Message:   message,
		ChannelID: topic.ChannelID,
	}

	if msgType, ok := message["type"].(string); ok {
		msg.Type = msgType
	}

	if msgData, ok := message["data"].(map[string]interface{}); ok {
		msg.Data = msgData
	}

	msg.Timestamp = extractTimestamp(message, msg.Data)

	if msg.Data != nil {
		msg.ChannelID = extractChannelID(msg.Data, topic.ChannelID)
	}

	return msg, nil
}

func extractTimestamp(message, data map[string]interface{}) time.Time {
	if data != nil {
		if ts, ok := data["timestamp"].(string); ok {
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				return t
			}
		}
	}

	if ts, ok := message["server_time"].(float64); ok {
		return time.Unix(int64(ts), 0)
	}

	return time.Now()
}

func extractChannelID(data map[string]interface{}, defaultID string) string {
	if prediction, ok := data["prediction"].(map[string]interface{}); ok {
		if id, ok := prediction["channel_id"].(string); ok {
			return id
		}
	}
	if claim, ok := data["claim"].(map[string]interface{}); ok {
		if id, ok := claim["channel_id"].(string); ok {
			return id
		}
	}
	if id, ok := data["channel_id"].(string); ok {
		return id
	}
	if balance, ok := data["balance"].(map[string]interface{}); ok {
		if id, ok := balance["channel_id"].(string); ok {
			return id
		}
	}
	return defaultID
}
