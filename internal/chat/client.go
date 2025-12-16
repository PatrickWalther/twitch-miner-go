package chat

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/PatrickWalther/twitch-miner-go/internal/constants"
	"github.com/PatrickWalther/twitch-miner-go/internal/models"
)

type IRCClient struct {
	username  string
	token     string
	channel   string
	streamer  *models.Streamer

	conn     net.Conn
	reader   *bufio.Reader
	running  bool
	stopChan chan struct{}

	mu sync.RWMutex
}

func NewIRCClient(username, token string, streamer *models.Streamer) *IRCClient {
	return &IRCClient{
		username: username,
		token:    token,
		channel:  "#" + strings.ToLower(streamer.Username),
		streamer: streamer,
		stopChan: make(chan struct{}),
	}
}

func (c *IRCClient) Connect() error {
	addr := net.JoinHostPort(constants.IRCURL, fmt.Sprintf("%d", constants.IRCPort))
	conn, err := net.DialTimeout("tcp", addr, 30*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to IRC: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.reader = bufio.NewReader(conn)
	c.running = true
	c.mu.Unlock()

	if err := c.authenticate(); err != nil {
		c.conn.Close()
		return fmt.Errorf("failed to authenticate: %w", err)
	}

	if err := c.join(); err != nil {
		c.conn.Close()
		return fmt.Errorf("failed to join channel: %w", err)
	}

	go c.readLoop()

	slog.Info("Joined IRC chat", "channel", c.channel)
	return nil
}

func (c *IRCClient) authenticate() error {
	if err := c.send(fmt.Sprintf("PASS oauth:%s", c.token)); err != nil {
		return err
	}
	return c.send(fmt.Sprintf("NICK %s", c.username))
}

func (c *IRCClient) join() error {
	return c.send(fmt.Sprintf("JOIN %s", c.channel))
}

func (c *IRCClient) send(message string) error {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("not connected")
	}

	_, err := conn.Write([]byte(message + "\r\n"))
	return err
}

func (c *IRCClient) readLoop() {
	for {
		select {
		case <-c.stopChan:
			return
		default:
		}

		c.mu.RLock()
		reader := c.reader
		running := c.running
		c.mu.RUnlock()

		if !running || reader == nil {
			return
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			c.mu.RLock()
			running := c.running
			c.mu.RUnlock()

			if running {
				slog.Debug("IRC read error", "channel", c.channel, "error", err)
			}
			return
		}

		line = strings.TrimSpace(line)
		c.handleMessage(line)
	}
}

func (c *IRCClient) handleMessage(line string) {
	if strings.HasPrefix(line, "PING") {
		pongMsg := strings.Replace(line, "PING", "PONG", 1)
		_ = c.send(pongMsg)
		return
	}

	if strings.Contains(line, "PRIVMSG") {
		c.handlePrivMsg(line)
	}
}

func (c *IRCClient) handlePrivMsg(line string) {
	parts := strings.SplitN(line, " ", 4)
	if len(parts) < 4 {
		return
	}

	prefix := parts[0]
	message := strings.TrimPrefix(parts[3], ":")

	nick := ""
	if strings.HasPrefix(prefix, ":") {
		prefix = prefix[1:]
		if idx := strings.Index(prefix, "!"); idx > 0 {
			nick = prefix[:idx]
		}
	}

	mention := "@" + strings.ToLower(c.username)
	if strings.Contains(strings.ToLower(message), mention) ||
		strings.Contains(strings.ToLower(message), strings.ToLower(c.username)) {
		slog.Info("Chat mention",
			"channel", c.channel,
			"from", nick,
			"message", message,
		)
	}
}

func (c *IRCClient) Stop() {
	c.mu.Lock()
	c.running = false
	c.mu.Unlock()

	close(c.stopChan)

	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn != nil {
		_ = c.send("PART " + c.channel)
		conn.Close()
	}

	slog.Info("Left IRC chat", "channel", c.channel)
}

func (c *IRCClient) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}
