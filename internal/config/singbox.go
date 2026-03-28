package config

import (
	"encoding/json"
	"fmt"
)

func BuildConfig(servers []VlessConfig) ([]byte, error) {
	if len(servers) == 0 {
		return nil, fmt.Errorf("no servers provided")
	}

	var serverTags []string
	var vlessOutbounds []map[string]any

	for i, s := range servers {
		tag := fmt.Sprintf("server-%d", i)
		serverTags = append(serverTags, tag)
		vlessOutbounds = append(vlessOutbounds, buildVlessOutbound(tag, s))
	}

	selectorOutbounds := append([]string{"auto"}, serverTags...)

	outbounds := []map[string]any{
		{
			"type":      "selector",
			"tag":       "proxy",
			"outbounds": selectorOutbounds,
			"default":   "auto",
		},
		{
			"type":                        "urltest",
			"tag":                         "auto",
			"outbounds":                   serverTags,
			"url":                         "https://www.gstatic.com/generate_204",
			"interval":                    "3m",
			"tolerance":                   50,
			"interrupt_exist_connections": true,
		},
	}
	for _, v := range vlessOutbounds {
		outbounds = append(outbounds, v)
	}
	outbounds = append(outbounds,
		map[string]any{"type": "direct", "tag": "direct"},
		map[string]any{"type": "block", "tag": "block"},
		map[string]any{"type": "dns", "tag": "dns-out"},
	)

	config := map[string]any{
		"log": map[string]any{
			"level":     "info",
			"timestamp": true,
		},
		"dns": map[string]any{
			"servers": []map[string]any{
				{"type": "tls", "tag": "dns-remote", "server": "8.8.8.8"},
				{"type": "local", "tag": "dns-local"},
			},
			"rules": []map[string]any{
				{"outbound": "any", "server": "dns-local"},
			},
			"final": "dns-remote",
		},
		"inbounds": []map[string]any{
			{
				"type":         "tun",
				"tag":          "tun-in",
				"address":      []string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"},
				"auto_route":   true,
				"strict_route": true,
				"stack":        "mixed",
			},
		},
		"outbounds": outbounds,
		"route": map[string]any{
			"rules": []map[string]any{
				{"action": "sniff"},
				{"protocol": "dns", "action": "hijack-dns"},
				{"ip_is_private": true, "outbound": "direct"},
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
				"enabled": true,
				"path":    "cache.db",
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
		if s.Fingerprint != "" {
			tls["utls"] = map[string]any{
				"enabled":     true,
				"fingerprint": s.Fingerprint,
			}
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
		if s.Mode != "" {
			transport["mode"] = s.Mode
		}
		out["transport"] = transport
	}

	return out
}
