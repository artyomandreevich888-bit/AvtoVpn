package mobile

import (
	"context"
	"encoding/json"
	"io"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

var lastStatusJSON = "{\"State\": \"disconnected\", \"Server\": \"\", \"Delay\": 0, \"AliveCount\": 0, \"TotalCount\": 0, \"Error\": \"\"}"
var lastStatusJSONMu sync.RWMutex

func updateLastStatus(state int, server string, delay, alive, total int, errMsg string) {
	stateStr := "disconnected"
	switch state {
	case StateFetching:
		stateStr = "fetching"
	case StateStarting:
		stateStr = "starting"
	case StateConnected:
		stateStr = "connected"
	case StateError:
		stateStr = "error"
	}
	b, _ := json.Marshal(map[string]interface{}{
		"State": stateStr, "Server": server, "Delay": delay,
		"AliveCount": alive, "TotalCount": total, "Error": errMsg,
	})
	lastStatusJSONMu.Lock()
	lastStatusJSON = string(b)
	lastStatusJSONMu.Unlock()
}

func GetStatusJSON() string {
	lastStatusJSONMu.RLock()
	defer lastStatusJSONMu.RUnlock()
	return lastStatusJSON
}

func CheckServicesJSON() string {
	mu.Lock()
	m := mgr
	mu.Unlock()
	if m == nil { return "[]" }
	checks := m.CheckServices(context.Background())
	type svc struct{ Name, Status string; Delay int }
	list := make([]svc, len(checks))
	for i, c := range checks { list[i] = svc{c.Name, c.Status, c.Delay} }
	b, _ := json.Marshal(list)
	return string(b)
}

func GetLocationInfoJSON() string {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("http://ip-api.com/json/")
	if err != nil { return "" }
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var raw map[string]interface{}
	if json.Unmarshal(body, &raw) != nil { return "" }
	b, _ := json.Marshal(map[string]interface{}{"IP": raw["query"], "Country": raw["country"], "CountryCode": raw["countryCode"]})
	return string(b)
}

func GetServerListJSON() string {
	mu.Lock(); m := mgr; mu.Unlock()
	if m == nil || m.ClashAPI == nil { return "[]" }
	servers := m.GetServerList(context.Background())
	type srv struct{ Tag, Name string; Delay int; Alive, Active bool }
	list := make([]srv, len(servers))
	for i, s := range servers { list[i] = srv{s.Tag, s.Name, s.Delay, s.Alive, s.Active} }
	b, _ := json.Marshal(list)
	return string(b)
}

func SelectServerByTag(tag string) {
	mu.Lock(); m := mgr; mu.Unlock()
	if m != nil { _ = m.SelectServer(context.Background(), tag) }
}

const adBannerURL = "https://cdn.jsdelivr.net/gh/artyomandreevich888-bit/autovpn-config@main/ad.json"

func GetAdBannerJSON() string {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(adBannerURL)
	if err != nil { return "" }
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var raw map[string]json.RawMessage
	if json.Unmarshal(body, &raw) != nil { return "" }
	if adsRaw, ok := raw["ads"]; ok {
		var ads []map[string]json.RawMessage
		if json.Unmarshal(adsRaw, &ads) != nil || len(ads) == 0 { return "" }
		var vis []map[string]json.RawMessage
		for _, ad := range ads {
			var v bool
			if vr, ok := ad["visible"]; ok { json.Unmarshal(vr, &v) }
			if v { vis = append(vis, ad) }
		}
		if len(vis) == 0 { return "" }
		b, _ := json.Marshal(vis[rand.Intn(len(vis))])
		return string(b)
	}
	var chk map[string]interface{}
	if json.Unmarshal(body, &chk) != nil { return "" }
	if v, _ := chk["visible"].(bool); !v { return "" }
	return string(body)
}
