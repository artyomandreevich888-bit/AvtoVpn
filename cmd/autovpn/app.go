package main

import (
	"context"
	"os/exec"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"math/rand"
	"time"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/mewmewmemw/autovpn/internal/app"
	"github.com/mewmewmemw/autovpn/internal/config"
)

// App struct is exposed to the frontend via Wails bindings.
type App struct {
	ctx      context.Context
	manager  *app.Manager
	cacheDir string
}

func NewApp() *App {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	cacheDir = filepath.Join(cacheDir, "autovpn")

	return &App{
		manager: app.NewManager(&config.Fetcher{
			CacheDir: cacheDir,
		}),
		cacheDir: cacheDir,
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// Clean up stale TUN interface from previous run
	cleanupTUN()
}

func cleanupTUN() {
	// Remove IP address first, then delete the adapter
	exec.Command("powershell", "-Command", "Get-NetAdapter -Name tun0 -ErrorAction SilentlyContinue | Remove-NetAdapter -Confirm:$false").Run()
	exec.Command("netsh", "interface", "ip", "delete", "address", "name=tun0", "addr=172.19.0.1").Run()
	// netsh interface delete cleans up leftover tun0
	// small wait for cleanup to take effect
	time.Sleep(500 * time.Millisecond)
}

type StatusResult struct {
	State      string `json:"State"`
	Server     string `json:"Server"`
	Delay      int    `json:"Delay"`
	AliveCount int    `json:"AliveCount"`
	TotalCount int    `json:"TotalCount"`
	Error      string `json:"Error"`
}

// ConnectAsync starts VPN connection in background. Poll GetStatus() for progress.
func (a *App) ConnectAsync() {
	go func() {
		a.manager.Connect()
	}()
}

// Connect starts the VPN. Returns empty string on success, error message on failure.
func (a *App) Connect() string {
	if err := a.manager.Connect(); err != nil {
		return err.Error()
	}
	return ""
}

// Disconnect stops the VPN.
func (a *App) Disconnect() {
	a.manager.Disconnect()
}

// GetStatus returns current connection status.
func (a *App) GetStatus() StatusResult {
	s := a.manager.Status()
	return StatusResult{
		State:      string(s.State),
		Server:     s.Server,
		Delay:      s.Delay,
		AliveCount: s.AliveCount,
		TotalCount: s.TotalCount,
		Error:      s.Error,
	}
}

type LocationInfo struct {
	IP          string `json:"IP"`
	Country     string `json:"Country"`
	CountryCode string `json:"CountryCode"`
	City        string `json:"City"`
}

// fetchLocationFromURL tries to fetch location info from a given API URL.
func fetchLocationFromURL(client *http.Client, apiURL string) LocationInfo {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, apiURL, nil)
	if err != nil {
		return LocationInfo{}
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return LocationInfo{}
	}
	defer resp.Body.Close()

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return LocationInfo{}
	}

	getString := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := raw[k]; ok {
				if s, ok := v.(string); ok && s != "" {
					return s
				}
			}
		}
		return ""
	}

	ip := getString("query", "ip")
	country := getString("country", "country_name")
	countryCode := getString("countryCode", "country_code")
	city := getString("city")

	if ip == "" {
		return LocationInfo{}
	}
	return LocationInfo{
		IP:          ip,
		Country:     country,
		CountryCode: countryCode,
		City:        city,
	}
}

// GetLocationInfo returns external IP and country info through the VPN proxy.
// Tries multiple APIs and falls back to direct connection if proxy fails.
func (a *App) GetLocationInfo() LocationInfo {
	type attempt struct {
		apiURL string
		client *http.Client
	}

	proxyURL, _ := url.Parse("http://127.0.0.1:7890")
	proxyClient := &http.Client{
		Timeout:   8 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
	}
	directClient := &http.Client{
		Timeout: 8 * time.Second,
	}

	attempts := []attempt{
		{"http://ip-api.com/json/?fields=query,country,countryCode,city", proxyClient},
		{"http://ipwho.is/", proxyClient},
		{"https://ipinfo.io/json", proxyClient},
		{"http://ip-api.com/json/?fields=query,country,countryCode,city", directClient},
		{"http://ipwho.is/", directClient},
	}

	for _, att := range attempts {
		info := fetchLocationFromURL(att.client, att.apiURL)
		if info.IP != "" {
			return info
		}
	}
	return LocationInfo{}
}

// GetExternalIP returns the current external IP address.
func (a *App) GetExternalIP() string {
	info := a.GetLocationInfo()
	return info.IP
}

// SetWindowTitle updates the window title bar.
func (a *App) SetWindowTitle(title string) {
	if a.ctx != nil {
		wailsRuntime.WindowSetTitle(a.ctx, title)
	}
}

type ServiceCheckResult struct {
	Name   string `json:"Name"`
	URL    string `json:"URL"`
	Status string `json:"Status"`
	Delay  int    `json:"Delay"`
}

// CheckServices tests connectivity to key services through the VPN.
func (a *App) CheckServices() []ServiceCheckResult {
	checks := a.manager.CheckServices(context.Background())
	results := make([]ServiceCheckResult, len(checks))
	for i, c := range checks {
		results[i] = ServiceCheckResult{
			Name:   c.Name,
			URL:    c.URL,
			Status: c.Status,
			Delay:  c.Delay,
		}
	}
	return results
}


// ServerItem is a single server entry for the frontend.
type ServerItem struct {
	Tag    string `json:"Tag"`
	Name   string `json:"Name"`
	Delay  int    `json:"Delay"`
	Active bool   `json:"Active"`
	Alive  bool   `json:"Alive"`
}

// GetServerList returns all servers with their ping and active state.
func (a *App) GetServerList() []ServerItem {
	items := a.manager.GetServerList(context.Background())
	result := make([]ServerItem, len(items))
	for i, item := range items {
		result[i] = ServerItem{
			Tag:    item.Tag,
			Name:   item.Name,
			Delay:  item.Delay,
			Active: item.Active,
			Alive:  item.Alive,
		}
	}
	return result
}


// SelectServer switches VPN to specified server tag.
func (a *App) SelectServer(tag string) string {
	if err := a.manager.SelectServer(context.Background(), tag); err != nil {
		return err.Error()
	}
	return ""
}

// Notify sends a Windows toast notification.
func (a *App) Notify(title, message string) {
	sendNotification(title, message)
}


// GetKillSwitch returns current kill switch state.
func (a *App) GetKillSwitch() bool {
	return a.manager.GetKillSwitch()
}

// SetKillSwitch enables or disables kill switch and reconnects if active.
func (a *App) SetKillSwitch(enabled bool) {
	a.manager.SetKillSwitch(enabled)
	a.saveKillSwitch(enabled)
	if a.manager.Engine.IsRunning() {
		go func() {
			a.manager.Disconnect()
			a.manager.Connect()
		}()
	}
}

func (a *App) saveKillSwitch(enabled bool) {
	// state is held in manager; nothing to persist separately
}

// SetWindowSize resizes the main window.
func (a *App) SetWindowSize(width, height int) {
	wailsRuntime.WindowSetMinSize(a.ctx, width, height)
	wailsRuntime.WindowSetSize(a.ctx, width, height)
}

// --- Feature: Auto-connect on startup ---

type appSettings struct {
	AutoConnect bool `json:"auto_connect"`
}

func (a *App) settingsPath() string {
	return filepath.Join(a.cacheDir, "settings.json")
}

func (a *App) loadSettings() appSettings {
	data, err := os.ReadFile(a.settingsPath())
	if err != nil {
		return appSettings{}
	}
	var s appSettings
	json.Unmarshal(data, &s)
	return s
}

func (a *App) saveSettings(s appSettings) error {
	os.MkdirAll(a.cacheDir, 0755)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.settingsPath(), data, 0644)
}

// GetAutoConnect returns true if auto-connect on startup is enabled.
func (a *App) GetAutoConnect() bool {
	return a.loadSettings().AutoConnect
}

// SetAutoConnect enables or disables auto-connect on startup.
func (a *App) SetAutoConnect(enabled bool) {
	s := a.loadSettings()
	s.AutoConnect = enabled
	a.saveSettings(s)
}

// --- Feature: Traffic speed ---

type TrafficSpeed struct {
	Up   int64 `json:"Up"`
	Down int64 `json:"Down"`
}

// GetTrafficSpeed returns current upload/download speed in bytes/sec.
func (a *App) GetTrafficSpeed() TrafficSpeed {
	snap, err := a.manager.ClashAPI.GetTraffic(context.Background())
	if err != nil {
		return TrafficSpeed{}
	}
	return TrafficSpeed{Up: snap.Up, Down: snap.Down}
}

// --- Feature: Connection history ---

type ConnectionRecord struct {
	Time     string `json:"Time"`
	Server   string `json:"Server"`
	Country  string `json:"Country"`
	Duration int64  `json:"Duration"`
}

func (a *App) historyPath() string {
	return filepath.Join(a.cacheDir, "history.json")
}

// GetHistory returns last 5 connection records (newest first).
func (a *App) GetHistory() []ConnectionRecord {
	data, err := os.ReadFile(a.historyPath())
	if err != nil {
		return []ConnectionRecord{}
	}
	var records []ConnectionRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return []ConnectionRecord{}
	}
	if len(records) > 5 {
		records = records[len(records)-5:]
	}
	// Reverse for newest-first display
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}
	return records
}

// SaveConnectionRecord adds a connection record to history.
func (a *App) SaveConnectionRecord(server, country string, durationSec int64) {
	data, _ := os.ReadFile(a.historyPath())
	var records []ConnectionRecord
	json.Unmarshal(data, &records)

	records = append(records, ConnectionRecord{
		Time:     time.Now().Format("02.01.2006 15:04"),
		Server:   server,
		Country:  country,
		Duration: durationSec,
	})

	if len(records) > 50 {
		records = records[len(records)-50:]
	}

	os.MkdirAll(a.cacheDir, 0755)
	out, _ := json.MarshalIndent(records, "", "  ")
	os.WriteFile(a.historyPath(), out, 0644)
}

// --- Feature: Refresh servers ---

// RefreshServers clears cache and reconnects if currently connected.
func (a *App) RefreshServers() string {
	cacheFile := filepath.Join(a.manager.Fetcher.CacheDir, "configs.txt")
	os.Remove(cacheFile)
	if a.manager.Engine.IsRunning() {
		go func() {
			a.manager.Disconnect()
			a.manager.Connect()
		}()
	}
	return ""
}

// --- Window management for tray ---


// adBannerURL is the remote URL for the ad banner config.
// Update ad.json in the repo to control what's shown to users.
const adBannerURL = "https://cdn.jsdelivr.net/gh/artyomandreevich888-bit/autovpn-config@main/ad.json"

// AdBanner holds the ad banner content fetched from adBannerURL.
type AdBanner struct {
	Visible  bool   `json:"visible"`
	Label    string `json:"label"`
	Title    string `json:"title"`
	Text     string `json:"text"`
	Button   string `json:"button"`
	Link     string `json:"link"`
	Color    string `json:"color"`
	ImageURL string `json:"image_url"`
	VideoURL string `json:"video_url"`
	Duration int    `json:"duration"`
}

func (a *App) GetAdBanner() AdBanner {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(adBannerURL)
	if err != nil { return AdBanner{} }
	defer resp.Body.Close()
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil { return AdBanner{} }
	if adsRaw, ok := raw["ads"]; ok {
		var ads []AdBanner
		if err := json.Unmarshal(adsRaw, &ads); err != nil || len(ads) == 0 { return AdBanner{} }
		var vis []AdBanner
		for _, ad := range ads { if ad.Visible { vis = append(vis, ad) } }
		if len(vis) == 0 { return AdBanner{} }
		return vis[rand.Intn(len(vis))]
	}
	body, _ := json.Marshal(raw)
	var banner AdBanner
	if json.Unmarshal(body, &banner) != nil { return AdBanner{} }
	return banner
}


// allowedURL is the remote URL to check if this app version is allowed.
const allowedURL = "https://cdn.jsdelivr.net/gh/artyomandreevich888-bit/autovpn-config@main/allowed.json"

// AppAllowedResult holds the server-side allow/block status.
type AppAllowedResult struct {
	Allowed bool   `json:"allowed"`
	Message string `json:"message"`
}

// CheckAppAllowed fetches the remote allowed.json to verify this build is permitted.
func (a *App) CheckAppAllowed() AppAllowedResult {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(allowedURL)
	if err != nil {
		return AppAllowedResult{Allowed: true}
	}
	defer resp.Body.Close()
	var result AppAllowedResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return AppAllowedResult{Allowed: true}
	}
	return result
}

// OpenURL opens a URL in the default system browser.
func (a *App) OpenURL(rawURL string) {
	exec.Command("cmd", "/c", "start", "", rawURL).Start()
}

// HideWindow hides the main window (minimize to tray).
func (a *App) HideWindow() {
	wailsRuntime.WindowHide(a.ctx)
}

// ShowWindow shows the main window.
func (a *App) ShowWindow() {
	wailsRuntime.WindowShow(a.ctx)
}
