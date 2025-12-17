package analytics

import "github.com/PatrickWalther/twitch-miner-go/internal/chat"

type ChatLoggerAdapter struct {
	server *AnalyticsServer
}

func NewChatLoggerAdapter(server *AnalyticsServer) *ChatLoggerAdapter {
	return &ChatLoggerAdapter{server: server}
}

func (a *ChatLoggerAdapter) RecordChatMessage(streamer string, msg chat.ChatMessageData) error {
	return a.server.RecordChatMessage(streamer, msg.Username, msg.DisplayName, msg.Message, msg.Emotes, msg.Badges, msg.Color)
}
