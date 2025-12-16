package constants

const (
	TwitchURL      = "https://www.twitch.tv"
	GQLURL         = "https://gql.twitch.tv/gql"
	PubSubURL      = "wss://pubsub-edge.twitch.tv/v1"
	OAuthDeviceURL = "https://id.twitch.tv/oauth2/device"
	OAuthTokenURL  = "https://id.twitch.tv/oauth2/token"
	IRCURL         = "irc.chat.twitch.tv"
	IRCPort        = 6667
	IRCPortTLS     = 6697
	UsherURL       = "https://usher.ttvnw.net"

	ClientIDTV      = "ue6666qo983tsx6so1t0vnawi233wa"
	ClientIDBrowser = "kimne78kx3ncx6brgo4mv6wki5h1ko"
	ClientIDMobile  = "r8s4dac0uhzifbpu9sjdiwzctle17ff"

	DefaultClientVersion = "ef928475-9403-42f2-8a34-55784bd08e16"

	TVUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/108.0.0.0 Safari/537.36"

	MaxTopicsPerConnection = 50
	MaxSimultaneousStreams = 2
)

var OAuthScopes = "channel_read chat:read user_blocks_edit user_blocks_read user_follows_edit user_read"
