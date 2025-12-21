# Twitch Channel Points Miner

[![CI](https://github.com/PatrickWalther/twitch-miner-go/actions/workflows/ci.yml/badge.svg)](https://github.com/PatrickWalther/twitch-miner-go/actions/workflows/ci.yml)
[![Release](https://github.com/PatrickWalther/twitch-miner-go/actions/workflows/release.yml/badge.svg)](https://github.com/PatrickWalther/twitch-miner-go/releases)
[![Docker](https://img.shields.io/docker/v/thegame402/twitch-miner-go?label=docker)](https://hub.docker.com/r/thegame402/twitch-miner-go)

A Go rewrite of [Twitch-Channel-Points-Miner-v2](https://github.com/rdavydov/Twitch-Channel-Points-Miner-v2). This rewrite was done for performance and size reasons - the Docker image is over 85x smaller (only ~5MB vs 440MB) and standalone binaries at under 5MB each.

This tool passively earns Twitch channel points by simulating viewer presence across multiple streams.

## Features

- **Passive Point Farming**: Earn channel points (+10-12 every 5 minutes) by simulating watch time
- **Automatic Bonus Claiming**: Auto-claim +50 point bonuses when available
- **Watch Streak Detection**: Catch +450 point watch streaks across streamers
- **Raid Following**: Automatically join raids for +250 points
- **Prediction Betting**: Intelligent automated betting on channel predictions with multiple strategies
- **Game Drops**: Track and claim game drops from inventory
- **Moments Claiming**: Automatically claim Twitch Moments when available
- **Community Goals**: Contribute channel points to streamer community goals
- **Multi-Streamer Support**: Monitor multiple streamers with priority-based scheduling
- **Real-time Analytics**: Web-based dashboard for tracking point earnings
- **Discord Notifications**: Get notified for mentions, point goals, and stream status changes

## Installation

### Download Binary

Download the latest release for your platform from the [Releases page](https://github.com/PatrickWalther/twitch-miner-go/releases).

### Build from Source

```bash
# Clone the repository
git clone https://github.com/PatrickWalther/twitch-miner-go.git
cd twitch-miner-go

# Build with version info
make build

# Build with UPX compression (smallest size)
make build-compressed

# Or build manually
go build -ldflags "-s -w -X github.com/PatrickWalther/twitch-miner-go/internal/version.Version=$(git describe --tags)" -o twitch-miner-go ./cmd/miner

# Build for all platforms
make build-all
```

### Docker

```bash
# Pull from Docker Hub
docker pull thegame402/twitch-miner-go:latest

# Or from GitHub Container Registry
docker pull ghcr.io/patrickwalther/twitch-miner-go:latest

# Run with volume mounts
docker run -d \
  -v /path/to/config:/config \
  -v /path/to/cookies:/cookies \
  -v /path/to/logs:/logs \
  -v /path/to/database:/database \
  -p 5000:5000 \
  thegame402/twitch-miner-go:latest
```

### Unraid

1. Install the **Community Applications** plugin from the Apps tab
2. Search for "twitch-miner-go" in Community Applications
3. Click Install and configure paths

Alternatively, manually add the container via Docker tab → Add Container using `thegame402/twitch-miner-go:latest` as the repository.

## Configuration

Generate a sample configuration file:

```bash
./twitch-miner-go -generate-config
```

This creates `config.sample.json`. Rename it to `config.json` and update with your settings:

```json
{
  "username": "your_twitch_username",
  "claimDropsOnStartup": false,
  "enableAnalytics": true,
  "priority": ["STREAK", "DROPS", "ORDER"],
  "streamerSettings": {
    "makePredictions": true,
    "followRaid": true,
    "claimDrops": true,
    "claimMoments": true,
    "watchStreak": true,
    "communityGoals": false,
    "chat": "ONLINE",
    "bet": {
      "strategy": "SMART",
      "percentage": 5,
      "percentageGap": 20,
      "maxPoints": 50000,
      "minimumPoints": 0,
      "stealthMode": false,
      "delay": 6,
      "delayMode": "FROM_END"
    }
  },
  "streamers": [
    { "username": "streamer1" },
    { "username": "streamer2" }
  ],
  "rateLimits": {
    "websocketPingInterval": 27,
    "campaignSyncInterval": 60,
    "minuteWatchedInterval": 60,
    "requestDelay": 0.5,
    "reconnectDelay": 60,
    "streamCheckInterval": 600
  }
}
```

## Usage

```bash
# Run with default config.json
./twitch-miner-go

# Run with custom config file
./twitch-miner-go -config path/to/config.json

# Enable debug logging
./twitch-miner-go -debug
```

On first run, you'll be prompted to authenticate via Twitch's device flow:
1. Open https://www.twitch.tv/activate
2. Enter the code displayed
3. The application will automatically continue once authenticated

## Priority System

The application watches up to 2 streams simultaneously, selected by priority:

| Priority | Behavior |
|----------|----------|
| `STREAK` | Prioritize streamers with pending watch streak |
| `DROPS` | Prioritize streamers with active drop campaigns |
| `SUBSCRIBED` | Prioritize subscribed channels |
| `ORDER` | Follow order in streamers list |
| `POINTS_ASCENDING` | Lowest points first |
| `POINTS_DESCENDING` | Highest points first |

## Betting Strategies

| Strategy | Logic |
|----------|-------|
| `MOST_VOTED` | Choose option with most users |
| `HIGH_ODDS` | Choose option with highest odds |
| `PERCENTAGE` | Choose option with highest win percentage |
| `SMART_MONEY` | Choose option with highest top bet |
| `SMART` | If user gap > percentageGap: follow majority; else: choose highest odds |
| `NUMBER_1` - `NUMBER_8` | Always choose specific outcome position |

## Chat Presence Modes

| Mode | Behavior |
|------|----------|
| `ALWAYS` | Always connected to IRC |
| `NEVER` | Never connect to IRC |
| `ONLINE` | Connect when streamer is online |
| `OFFLINE` | Connect when streamer is offline |

## Web Dashboard

When enabled, the web server provides a dashboard at `http://localhost:5000` with:
- **Dashboard**: Overview of all streamers with current points and today's earnings
- **Streamer Pages**: Historical point data with interactive charts
- **Settings**: Runtime configuration (can be changed without restart)
- **Notifications**: Discord notification management (when Discord is enabled)
- **Chat Logs**: Searchable chat history per streamer (optional)

### Web Dashboard Configuration

```json
{
  "analytics": {
    "host": "0.0.0.0",
    "port": 5000,
    "refresh": 5,
    "daysAgo": 7,
    "enableChatLogs": true
  }
}
```

| Setting | Default | Description |
|---------|---------|-------------|
| `host` | 0.0.0.0 | Server bind address |
| `port` | 5000 | Server port |
| `refresh` | 5 | Dashboard auto-refresh interval (minutes) |
| `daysAgo` | 7 | Default chart date range |
| `enableChatLogs` | false | Enable chat message logging |

### Chat Logging

When `enableChatLogs` is enabled:
- All chat messages from joined channels are stored in SQLite
- Messages include username, display name, emotes, badges, and color
- Searchable chat log on each streamer's dashboard page
- Per-streamer override available via `"chatLogs": true/false` in streamer settings

**Note**: Chat logging requires the streamer's chat to be joined (based on `chat` setting).

## Discord Notifications

The miner supports Discord notifications for:
- **Chat Mentions**: Get notified when someone mentions you in chat
- **Point Goals**: Get notified when you reach a point threshold
- **Stream Online/Offline**: Get notified when streamers go live or offline

### Setting Up Discord Bot

1. Go to the [Discord Developer Portal](https://discord.com/developers/applications)
2. Click **New Application** and give it a name
3. Go to the **Bot** section
4. Click **Reset Token** and copy the token (keep it secret!)
5. Enable **Message Content Intent** under Privileged Gateway Intents
6. Go to **OAuth2 → URL Generator**
7. Select the **bot** scope
8. Select these permissions:
   - Send Messages
   - Embed Links
   - Read Message History
9. Copy the generated URL and open it in your browser
10. Select your Discord server and authorize the bot

### Getting Your Guild ID

See [Discord's official guide](https://support.discord.com/hc/en-us/articles/206346498-Where-can-I-find-my-User-Server-Message-ID) for detailed instructions, or:

1. Enable Developer Mode in Discord (User Settings → Advanced → Developer Mode)
2. Right-click your server name and select **Copy Server ID**

### Configuration

Add Discord settings to your `config.json`:

```json
{
  "discord": {
    "enabled": true,
    "botToken": "YOUR_BOT_TOKEN",
    "guildId": "YOUR_SERVER_ID"
  }
}
```

After saving, a **Notifications** page will appear in the web dashboard where you can:
- Select which Discord channels to send each notification type to
- Enable/disable mention notifications (globally or per-streamer)
- Create point goal rules (with one-time or recurring options)
- Enable/disable online/offline notifications (globally or per-streamer)

## Rate Limits

All rate limits are configurable. Defaults are tuned to match the Python miner and avoid Twitch rate limiting:

| Setting | Default | Description |
|---------|---------|-------------|
| `websocketPingInterval` | 27 | Base seconds between WebSocket pings (20-60), ±2.5s random jitter applied |
| `campaignSyncInterval` | 60 | Minutes between drop campaign syncs (5-120) |
| `minuteWatchedInterval` | 60 | Base seconds for minute-watched cycle (30-120), divided by # of active streamers |
| `requestDelay` | 0.5 | Seconds between consecutive API calls (0.1-2.0) |
| `reconnectDelay` | 60 | Seconds to wait before reconnecting (30-300) |
| `streamCheckInterval` | 600 | Seconds between stream status checks (60-900) |

## License

GNU General Public License v3.0 - See LICENSE file for details.

## Disclaimer

This tool is for educational purposes. Use responsibly and in accordance with Twitch's Terms of Service.
