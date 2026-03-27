# VlessParser — спецификация

## Назначение

Парсинг VLESS URI строк из файла конфигов в структурированные объекты.

## Входной формат

Файл `BLACK_VLESS_RUS.txt` содержит:
- Комментарии: строки начинающиеся с `#` (заголовки файла, метаданные)
- Пустые строки
- VLESS URI: `vless://uuid@host:port?params#fragment`

Пример URI:
```
vless://0cfb8127-873d-46ca-9f31-f7918cdaaf11@xfer.mos-drywall.shop:443?type=xhttp&security=reality&encryption=none&fp=chrome&pbk=bPRyJNdN6kxTypxr65RjpadXexY5fztAecykcC-NrTM&sid=6a2f3e&sni=xfer.mos-drywall.shop&path=/devxhttp/&mode=packet-up#%F0%9F%87%A9%F0%9F%87%AA%20Germany%20Frankfurt%20%5BBL%5D
```

## Параметры URI (query string)

| Параметр | Описание | Примеры |
|----------|----------|---------|
| type | Транспорт | tcp, xhttp, ws, grpc |
| security | Тип шифрования | reality, tls, none |
| encryption | Шифрование VLESS | none |
| fp | uTLS fingerprint | chrome, firefox, safari |
| sni | Server Name Indication | domain.com |
| pbk | Reality public key | base64 строка |
| sid | Reality short ID | hex строка (0-16 символов) |
| path | Путь транспорта | /devxhttp/ |
| mode | Режим транспорта | packet-up |
| flow | XTLS flow | xtls-rprx-vision |

## Fragment (#)

URL-encoded строка с метаданными: `флаг Страна Город [тип]`
- Пример decoded: `🇩🇪 Germany Frankfurt [BL]`
- `[BL]` = black list, `[WL]` = white list

## API

### ParseVlessURI

```go
func ParseVlessURI(uri string) (VlessConfig, error)
```

Парсит одну VLESS URI строку. Возвращает ошибку если:
- URI не начинается с `vless://`
- Отсутствует host или port
- Port не число или вне диапазона 1-65535

### ParseConfigFile

```go
func ParseConfigFile(text string) ([]VlessConfig, []error)
```

Парсит весь файл. Пропускает пустые строки и строки начинающиеся с `#`.
Возвращает все успешно распарсенные конфиги + список ошибок для невалидных строк.
Не прерывается на ошибках — парсит всё что может.

### VlessConfig

```go
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
    RawURI      string // оригинальная строка
}
```

## Edge cases

- URI без query параметров → пустые строки для отсутствующих полей
- Fragment отсутствует → DisplayName = ""
- Дублирующиеся серверы (host:port) → оставляем все, дедупликация не здесь
- Невалидный URL-encoding в fragment → использовать raw fragment
