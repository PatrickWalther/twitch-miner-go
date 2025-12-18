package version

// Version is set at build time via -ldflags "-X github.com/PatrickWalther/twitch-miner-go/internal/version.Version=..."
var Version = "dev"

// RepoURL is the GitHub repository URL
const RepoURL = "https://github.com/PatrickWalther/twitch-miner-go"
