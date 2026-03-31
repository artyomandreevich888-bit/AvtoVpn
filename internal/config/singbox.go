package config

import (
	"encoding/json"
	"fmt"
)

var supportedTransports = map[string]bool{
	"":            true,
	"tcp":         true,
	"ws":          true,
	"grpc":        true,
	"http":        true,
	"httpupgrade": true,
	"quic":        true,
}

func BuildConfig(servers []VlessConfig, killSwitch ...bool) ([]byte, error) {
	ks := len(killSwitch) == 0 || killSwitch[0]
	if len(servers) == 0 {
		return nil, fmt.Errorf("no servers provided")
	}

	var serverTags []string
	var vlessOutbounds []map[string]any

	for i, s := range servers {
		if !supportedTransports[s.Transport] {
			continue
		}
		// Reality requires uTLS fingerprint and public key
		if s.Security == "reality" && (s.Fingerprint == "" || s.PublicKey == "") {
			continue
		}
		tag := fmt.Sprintf("server-%d", i)
		serverTags = append(serverTags, tag)
		vlessOutbounds = append(vlessOutbounds, buildVlessOutbound(tag, s))
	}

	if len(serverTags) == 0 {
		return nil, fmt.Errorf("no servers with supported transports")
	}

	// selectorTags: auto + all individual servers (for manual override)
	selectorTags := append([]string{"auto"}, serverTags...)

	outbounds := []map[string]any{
		{
			"type":      "selector",
			"tag":       "proxy",
			"outbounds": selectorTags,
			"default":   "auto",
		},
		{
			"type":      "urltest",
			"tag":       "auto",
			"outbounds": serverTags,
			"url":       "https://www.gstatic.com/generate_204",
			"interval":  "30s",
			"tolerance": 30,
		},
	}
	for _, v := range vlessOutbounds {
		outbounds = append(outbounds, v)
	}
	outbounds = append(outbounds,
		map[string]any{"type": "direct", "tag": "direct"},
		map[string]any{"type": "block", "tag": "block"},
	)

	config := map[string]any{
		"log": map[string]any{
			"level":     "debug",
			"timestamp": true,
		},
		"dns": map[string]any{
			"servers": []map[string]any{
				{
					"type":   "tls",
					"tag":    "dns-remote",
					"server": "8.8.8.8",
					"detour": "proxy",
				},
			},
			"final": "dns-remote",
		},
		"inbounds": []map[string]any{
			{
				"type":         "tun",
				"tag":          "tun-in",
				"address":      []string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"},
				"auto_route":   true,
				"strict_route": ks,
				"stack":        "mixed",
			},
			{
				"type":              "http",
				"tag":               "http-in",
				"listen":            "127.0.0.1",
				"listen_port":       7890,
			},
		},
		"outbounds": outbounds,
		"route": map[string]any{
			"rules": []map[string]any{
				{"action": "sniff"},
				{"protocol": "dns", "action": "hijack-dns"},
				{"ip_is_private": true, "outbound": "direct"},
				// Route DNS traffic directly — prevents chicken-and-egg
				// when all proxy servers are dead.
				{"ip_cidr": []string{"8.8.8.8/32", "8.8.4.4/32", "1.1.1.1/32"}, "port": 53, "outbound": "direct"},
			},
			"final":                 "proxy",
			"auto_detect_interface": true,
		},
		"experimental": map[string]any{
			"clash_api": map[string]any{
				"external_controller": "127.0.0.1:9090",
				"secret":              "autovpn",
			},
			"cache_file": map[string]any{
				"enabled": false,
			},
		},
	}

	return json.MarshalIndent(config, "", "  ")
}

func buildVlessOutbound(tag string, s VlessConfig) map[string]any {
	out := map[string]any{
		"type":        "vless",
		"tag":         tag,
		"server":      s.Host,
		"server_port": s.Port,
		"uuid":        s.UUID,
	}

	if s.Flow != "" {
		out["flow"] = s.Flow
	}

	if s.Security == "reality" || s.Security == "tls" {
		tls := map[string]any{
			"enabled":     true,
			"server_name": s.SNI,
		}
		fp := s.Fingerprint
		if fp == "" {
			fp = "chrome"
		}
		tls["utls"] = map[string]any{
			"enabled":     true,
			"fingerprint": fp,
		}
		if s.Security == "reality" {
			tls["reality"] = map[string]any{
				"enabled":    true,
				"public_key": s.PublicKey,
				"short_id":   s.ShortID,
			}
		}
		out["tls"] = tls
	}

	if s.Transport != "" && s.Transport != "tcp" {
		transport := map[string]any{
			"type": s.Transport,
		}
		if s.Path != "" {
			transport["path"] = s.Path
		}
		out["transport"] = transport
	}

	return out
}

// ServerNamesForConfigs returns a map from sing-box tag → display name
// using the same filtering logic as BuildConfig.
func ServerNamesForConfigs(servers []VlessConfig) map[string]string {
	m := make(map[string]string)
	for i, s := range servers {
		if !supportedTransports[s.Transport] {
			continue
		}
		if s.Security == "reality" && (s.Fingerprint == "" || s.PublicKey == "") {
			continue
		}
		tag := fmt.Sprintf("server-%d", i)
		name := s.DisplayName
		if name == "" {
			name = s.Host
		}
		m[tag] = name
	}
	return m
}
