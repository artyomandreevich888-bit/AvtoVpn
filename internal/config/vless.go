package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type VlessConfig struct {
	UUID        string
	Host        string
	Port        int
	Transport   string // tcp, xhttp, ws, grpc
	Security    string // reality, tls, none
	Encryption  string // none
	Fingerprint string // chrome, firefox
	SNI         string
	PublicKey   string // Reality pbk
	ShortID     string // Reality sid
	Path        string
	Mode        string // packet-up
	Flow        string // xtls-rprx-vision
	DisplayName string // decoded fragment
	RawURI      string
}

func ParseVlessURI(uri string) (VlessConfig, error) {
	rawURI := uri

	if !strings.HasPrefix(uri, "vless://") {
		return VlessConfig{}, fmt.Errorf("not a vless URI: %q", uri)
	}

	// url.Parse fails on invalid percent-encoding in fragment.
	// Strip fragment before parsing, handle it separately.
	rawFragment := ""
	if idx := strings.IndexByte(uri, '#'); idx != -1 {
		rawFragment = uri[idx+1:]
		uri = uri[:idx]
	}

	u, err := url.Parse(uri)
	if err != nil {
		return VlessConfig{}, fmt.Errorf("invalid URI: %w", err)
	}

	host := u.Hostname()
	if host == "" {
		return VlessConfig{}, fmt.Errorf("missing host in URI")
	}

	portStr := u.Port()
	if portStr == "" {
		return VlessConfig{}, fmt.Errorf("missing port in URI")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return VlessConfig{}, fmt.Errorf("invalid port: %q", portStr)
	}

	q := u.Query()

	fragment := rawFragment
	if decoded, err := url.QueryUnescape(rawFragment); err == nil {
		fragment = decoded
	}

	return VlessConfig{
		UUID:        u.User.Username(),
		Host:        host,
		Port:        port,
		Transport:   q.Get("type"),
		Security:    q.Get("security"),
		Encryption:  q.Get("encryption"),
		Fingerprint: q.Get("fp"),
		SNI:         q.Get("sni"),
		PublicKey:   q.Get("pbk"),
		ShortID:     q.Get("sid"),
		Path:        q.Get("path"),
		Mode:        q.Get("mode"),
		Flow:        q.Get("flow"),
		DisplayName: fragment,
		RawURI:      rawURI,
	}, nil
}

func ParseConfigFile(text string) ([]VlessConfig, []error) {
	var configs []VlessConfig
	var errs []error

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cfg, err := ParseVlessURI(line)
		if err != nil {
			errs = append(errs, fmt.Errorf("line %q: %w", line, err))
			continue
		}
		configs = append(configs, cfg)
	}

	return configs, errs
}
