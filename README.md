# Twitch Channel Points Miner

[![CI](https://github.com/PatrickWalther/twitch-miner-go/actions/workflows/ci.yml/badge.svg)](https://github.com/PatrickWalther/twitch-miner-go/actions/workflows/ci.yml)
[![Release](https://github.com/PatrickWalther/twitch-miner-go/actions/workflows/release.yml/badge.svg)](https://github.com/PatrickWalther/twitch-miner-go/releases)
[![Docker](https://img.shields.io/docker/v/thegame402/twitch-miner-go?label=docker)](https://hub.docker.com/r/thegame402/twitch-miner-go)

A Go rewrite of [Twitch-Channel-Points-Miner-v2](https://github.com/rdavydov/Twitch-Channel-Points-Miner-v2), including most features except notifications. This rewrite was done for performance and size reasons - the Docker image is over 80x smaller (only 5.5MB) and standalone binaries for all major operating systems are available at under 13MB each.

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

# Or build manually
go build -ldflags "-s -w -X main.version=$(git describe --tags)" -o twitch-miner-go ./cmd/miner

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
  -v /path/to/analytics:/analytics \
  -p 5000:5000 \
  thegame402/twitch-miner-go:latest
```

### Unraid

1. Go to Docker tab → Add Container → Template Repositories
2. Add: `https://github.com/PatrickWalther/twitch-miner-go`
3. Click "Add Container" and search for "twitch-miner-go"
4. Configure paths and save

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
    "campaignSyncInterval": 30,
    "minuteWatchedInterval": 20,
    "requestDelay": 0.5,
    "reconnectDelay": 60,
    "streamCheckInterval": 30
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

## Analytics

When enabled, the analytics server provides a web dashboard at `http://localhost:5000` showing:
- Current points per streamer
- Historical point data with interactive charts
- Prediction results and annotations
- Watch streak bonuses
- **Chat logging** with search functionality (optional)

### Analytics Configuration

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

## Rate Limits

All rate limits are configurable:

| Setting | Default | Description |
|---------|---------|-------------|
| `websocketPingInterval` | 27 | Seconds between WebSocket pings (20-60) |
| `campaignSyncInterval` | 30 | Minutes between drop campaign syncs (5-120) |
| `minuteWatchedInterval` | 20 | Seconds between minute-watched events (15-60) |
| `requestDelay` | 0.5 | Seconds between consecutive API calls (0.1-2.0) |
| `reconnectDelay` | 60 | Seconds to wait before reconnecting (30-300) |
| `streamCheckInterval` | 30 | Seconds between stream status checks (15-120) |

## License

GNU General Public License v3.0 - See LICENSE file for details.

## Disclaimer

This tool is for educational purposes. Use responsibly and in accordance with Twitch's Terms of Service.
