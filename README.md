# Twitch Channel Points Miner

A Go implementation of an automated Twitch channel points miner. This tool passively earns Twitch channel points by simulating viewer presence across multiple streams.

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

```bash
# Clone the repository
git clone https://github.com/patrickdappollonio/twitch-miner.git
cd twitch-miner

# Build the application
go build -o twitch-miner ./cmd/miner

# Or install directly
go install ./cmd/miner
```

## Configuration

Generate a sample configuration file:

```bash
./twitch-miner -generate-config
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
./twitch-miner

# Run with custom config file
./twitch-miner -config path/to/config.json

# Enable debug logging
./twitch-miner -debug
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
- Historical point data
- Prediction results
- Watch streak bonuses

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

MIT License - See LICENSE file for details.

## Disclaimer

This tool is for educational purposes. Use responsibly and in accordance with Twitch's Terms of Service.
