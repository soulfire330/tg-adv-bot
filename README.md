# tg-adv-bot

Читает одну публичную Telegram-группу (адрес — в `TG_CHANNEL`) через отдельный
аккаунт (MTProto), отбирает объявления по кейвордам и вердикту LLM и пересылает
совпадения в заданный чат.

## Как это работает

Раз в `POLL_INTERVAL` (30 с) сервис забирает из истории группы сообщения новее
последнего обработанного (`data/state.json`) и по каждому:

1. склеивает альбом в один пост (по `grouped_id`);
2. проверяет кейворды из `keywords.txt` — нет вхождения, пост отбрасывается без затрат;
3. спрашивает LLM (`prompt.md`, strict `json_schema`, ответ `{matches, reason}`);
4. если `matches` — пересылает исходное сообщение (с автором) получателю.

Ошибка LLM или форварда логируется, пост пропускается, поток продолжается.

## Запуск

```bash
cp .env.example .env    # TG_API_ID, TG_API_HASH, TG_PHONE, TG_RECIPIENT, LLM_*
$EDITOR prompt.md       # критерий отбора
./run.sh                # соберёт образ, создаст keywords.txt и поднимет сервис
./run.sh login          # один раз: код из Telegram + пароль 2FA
```

Аккаунт должен **состоять в группе** — вступление делает человек, сервис только
проверяет членство при старте. Сессия лежит в `data/session.json` (volume),
`data/state.json` — последний обработанный пост.

Первый запуск разбирает последние `BACKFILL=10` постов, дальше — только новые.

## Переменные

Все — в `.env.example`. Ключевое: `TG_CHANNEL` (группа), `TG_RECIPIENT` (`@username`
или `me`), `LLM_BASE_URL`/`LLM_MODEL`, `SOCKS5_PROXY` (socks5h для Telegram и LLM).

## Разработка

```bash
go test ./...
go run ./cmd/app login
```

Тесты не ходят в сеть: LLM мокается `httptest`, Telegram — интерфейсами
`Source`/`Forwarder` в `internal/service`.
