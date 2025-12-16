package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/patrickdappollonio/twitch-miner/internal/constants"
)

var (
	ErrBadCredentials   = errors.New("bad credentials")
	ErrExpiredCode      = errors.New("device code expired")
	ErrAuthorizationPending = errors.New("authorization pending")
)

type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        []string `json:"scope"`
	TokenType    string `json:"token_type"`
}

type StoredAuth struct {
	AuthToken string `json:"auth_token"`
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
}

type TwitchAuth struct {
	clientID  string
	deviceID  string
	username  string
	token     string
	userID    string
	client    *http.Client
}

func NewTwitchAuth(username, deviceID string) *TwitchAuth {
	return &TwitchAuth{
		clientID: constants.ClientIDTV,
		deviceID: deviceID,
		username: strings.ToLower(strings.TrimSpace(username)),
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (a *TwitchAuth) GetAuthToken() string {
	return a.token
}

func (a *TwitchAuth) GetUserID() string {
	return a.userID
}

func (a *TwitchAuth) GetUsername() string {
	return a.username
}

func (a *TwitchAuth) SetToken(token string) {
	a.token = token
}

func (a *TwitchAuth) SetUserID(userID string) {
	a.userID = userID
}

func (a *TwitchAuth) cookiesPath() string {
	return filepath.Join("cookies", fmt.Sprintf("%s.json", a.username))
}

func (a *TwitchAuth) LoadStoredAuth() error {
	data, err := os.ReadFile(a.cookiesPath())
	if err != nil {
		return err
	}

	var stored StoredAuth
	if err := json.Unmarshal(data, &stored); err != nil {
		return err
	}

	a.token = stored.AuthToken
	a.userID = stored.UserID
	a.username = stored.Username
	return nil
}

func (a *TwitchAuth) SaveAuth() error {
	if err := os.MkdirAll("cookies", 0755); err != nil {
		return err
	}

	stored := StoredAuth{
		AuthToken: a.token,
		UserID:    a.userID,
		Username:  a.username,
	}

	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(a.cookiesPath(), data, 0600)
}

func (a *TwitchAuth) DeleteStoredAuth() error {
	return os.Remove(a.cookiesPath())
}

func (a *TwitchAuth) HasStoredAuth() bool {
	_, err := os.Stat(a.cookiesPath())
	return err == nil
}

func (a *TwitchAuth) Login() error {
	if a.HasStoredAuth() {
		if err := a.LoadStoredAuth(); err == nil && a.token != "" {
			return nil
		}
	}

	return a.DeviceFlowLogin()
}

func (a *TwitchAuth) DeviceFlowLogin() error {
	deviceCode, err := a.requestDeviceCode()
	if err != nil {
		return fmt.Errorf("failed to get device code: %w", err)
	}

	fmt.Println("\n=== Twitch Login Required ===")
	fmt.Printf("Open: %s\n", deviceCode.VerificationURI)
	fmt.Printf("Enter code: %s\n", deviceCode.UserCode)
	fmt.Printf("Code expires in %d minutes\n", deviceCode.ExpiresIn/60)
	fmt.Println("Waiting for authorization...")

	token, err := a.pollForToken(deviceCode)
	if err != nil {
		return fmt.Errorf("failed to get token: %w", err)
	}

	a.token = token.AccessToken

	if err := a.SaveAuth(); err != nil {
		return fmt.Errorf("failed to save auth: %w", err)
	}

	return nil
}

func (a *TwitchAuth) requestDeviceCode() (*DeviceCodeResponse, error) {
	data := url.Values{
		"client_id": {a.clientID},
		"scopes":    {constants.OAuthScopes},
	}

	req, err := http.NewRequest("POST", constants.OAuthDeviceURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Client-Id", a.clientID)
	req.Header.Set("X-Device-Id", a.deviceID)
	req.Header.Set("User-Agent", constants.TVUserAgent)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var deviceCode DeviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&deviceCode); err != nil {
		return nil, err
	}

	return &deviceCode, nil
}

func (a *TwitchAuth) pollForToken(deviceCode *DeviceCodeResponse) (*TokenResponse, error) {
	deadline := time.Now().Add(time.Duration(deviceCode.ExpiresIn) * time.Second)
	interval := time.Duration(deviceCode.Interval) * time.Second

	for time.Now().Before(deadline) {
		time.Sleep(interval)

		token, err := a.requestToken(deviceCode.DeviceCode)
		if err == ErrAuthorizationPending {
			continue
		}
		if err != nil {
			return nil, err
		}

		return token, nil
	}

	return nil, ErrExpiredCode
}

func (a *TwitchAuth) requestToken(deviceCode string) (*TokenResponse, error) {
	data := url.Values{
		"client_id":   {a.clientID},
		"device_code": {deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}

	req, err := http.NewRequest("POST", constants.OAuthTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Client-Id", a.clientID)
	req.Header.Set("X-Device-Id", a.deviceID)
	req.Header.Set("User-Agent", constants.TVUserAgent)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusBadRequest {
		return nil, ErrAuthorizationPending
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var token TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, err
	}

	return &token, nil
}
