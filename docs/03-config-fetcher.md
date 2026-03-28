# ConfigFetcher — спецификация

## Назначение

Скачивание VLESS-конфигов из GitHub с fallback на CDN-зеркала и файловый кеш.

## API

### Fetcher

```go
type Fetcher struct {
    Client   *http.Client
    CacheDir string // директория для файлового кеша
}

func (f *Fetcher) Fetch(ctx context.Context) ([]VlessConfig, error)
```

## Стратегия fallback

Попробовать URL по порядку, первый успешный (HTTP 200 + непустое тело) — использовать:

1. `https://raw.githubusercontent.com/igareck/vpn-configs-for-russia/main/BLACK_VLESS_RUS.txt`
2. `https://cdn.jsdelivr.net/gh/igareck/vpn-configs-for-russia@main/BLACK_VLESS_RUS.txt`
3. `https://cdn.statically.io/gh/igareck/vpn-configs-for-russia/main/BLACK_VLESS_RUS.txt`

## Кеш

- При успешном скачивании — сохранить raw text в `{CacheDir}/configs.txt`
- При ошибке всех URL — попробовать прочитать кеш
- Если кеша нет — вернуть ошибку

## Парсинг

Использует `ParseConfigFile(text)` для разбора скачанного текста.
Ошибка если ни одного конфига не удалось распарсить.

## Edge cases

- HTTP 200 но пустое тело → пропустить, попробовать следующий URL
- HTTP 200 но 0 конфигов после парсинга → пропустить
- Таймаут → пропустить
- Кеш невалидный (0 конфигов) → ошибка
