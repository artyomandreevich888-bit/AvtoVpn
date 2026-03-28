# SingBoxConfigBuilder — спецификация

## Назначение

Генерация JSON-конфига sing-box из списка VlessConfig. Конфиг включает urltest (автотестирование + failover), selector (ручной override), tun (kill switch), clash_api (мониторинг).

## API

### BuildConfig

```go
func BuildConfig(servers []VlessConfig) ([]byte, error)
```

Принимает список серверов, возвращает JSON-конфиг sing-box. Ошибка если servers пуст.

## Структура генерируемого конфига

### Outbounds

Каждый VlessConfig → отдельный VLESS outbound:
- tag: `server-{index}` (server-0, server-1, ...)
- Параметры из VlessConfig: host, port, uuid, tls/reality, transport

Плюс служебные:
- `selector` tag="proxy": outbounds = ["auto", "server-0", "server-1", ...], default="auto"
- `urltest` tag="auto": outbounds = ["server-0", "server-1", ...], url, interval="3m", tolerance=50
- `direct` tag="direct"
- `block` tag="block"
- `dns` tag="dns-out"

### VLESS outbound mapping

```
VlessConfig.Host        → server
VlessConfig.Port        → server_port
VlessConfig.UUID        → uuid
VlessConfig.Flow        → flow (omit if empty)
VlessConfig.Security    → tls.enabled (true if "reality" or "tls")
VlessConfig.SNI         → tls.server_name
VlessConfig.Fingerprint → tls.utls.fingerprint
VlessConfig.PublicKey   → tls.reality.public_key (only if security="reality")
VlessConfig.ShortID     → tls.reality.short_id
VlessConfig.Transport   → transport.type (omit entire block if empty or "tcp")
VlessConfig.Path        → transport.path
VlessConfig.Mode        → transport.mode (only for xhttp)
```

### Inbounds

```json
{
  "type": "tun",
  "tag": "tun-in",
  "address": ["172.19.0.1/30", "fdfe:dcba:9876::1/126"],
  "auto_route": true,
  "strict_route": true,
  "stack": "mixed"
}
```

### DNS

```json
{
  "servers": [
    { "type": "tls", "tag": "dns-remote", "server": "8.8.8.8" },
    { "type": "local", "tag": "dns-local" }
  ],
  "rules": [{ "outbound": "any", "server": "dns-local" }],
  "final": "dns-remote"
}
```

### Route

```json
{
  "rules": [
    { "action": "sniff" },
    { "protocol": "dns", "action": "hijack-dns" },
    { "ip_is_private": true, "outbound": "direct" }
  ],
  "final": "proxy",
  "auto_detect_interface": true
}
```

### Experimental

```json
{
  "clash_api": {
    "external_controller": "127.0.0.1:9090",
    "secret": "autovpn"
  },
  "cache_file": { "enabled": true, "path": "cache.db" }
}
```

## Edge cases

- Один сервер → urltest с одним outbound (работает корректно)
- Transport = "tcp" или пустой → блок transport опускается
- Security = "" или "none" → tls не включается
- Flow = "" → поле flow опускается
- PublicKey/ShortID пустые при security="reality" → невалидный конфиг, но это ответственность источника данных, не билдера
