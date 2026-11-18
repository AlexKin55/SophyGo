# SophyGo

Telegram-бот — персональный AI-гид по достопримечательностям. Проект придумала и развивает София.

## Структура

```
travelbuddy/
├── bot/
│   ├── .env.bot                 # Переменные окружения бота
│   └── settings/bot.json        # Настройки бота
├── backend/
│   ├── .env.backend             # Переменные окружения backend
│   ├── internal/ai/             # SpeechKit STT → YandexGPT → TTS
│   └── settings/backend.json    # Настройки backend
├── internal/settings/           # Загрузчик JSON и .env
├── .gitignore
├── go.mod
└── README.md
```

## Требования

- Go 1.27+

## Настройки

У каждого приложения своя папка `settings/`:

- [`backend/settings/backend.json`](backend/settings/backend.json:1) — адрес и таймауты HTTP-сервера, пути endpoint'ов, лимит тела запроса;
- [`bot/settings/bot.json`](bot/settings/bot.json:1) — URL и путь backend, таймаут, количество ретраев и backoff, лимиты ответа, таймаут Telegram long polling.

Пример `backend/settings/backend.json`:

```json
{
  "server": {
    "addr": "127.0.0.1:8081",
    "read_header_timeout": "5s",
    "shutdown_timeout": "5s"
  },
  "api": {
    "health_path": "/health",
    "location_path": "/api/v1/guide/location",
    "max_location_body_bytes": 65536,
    "ai_session_path": "/api/v1/ai/session",
    "ai_session_stop_path": "/api/v1/ai/session/stop",
    "ai_voice_path": "/api/v1/ai/voice"
  },
  "ai": {
    "enabled": true,
    "api_key": "",
    "folder_id": "",
    "stt": {
      "endpoint": "https://stt.api.cloud.yandex.net/speech/v1/stt:recognize",
      "language": "ru-RU",
      "topic": "general",
      "timeout": "30s"
    },
    "gpt": {
      "endpoint": "https://llm.api.cloud.yandex.net/foundationModels/v1/completion",
      "model": "yandexgpt/latest",
      "temperature": 0.3,
      "max_tokens": 1000,
      "timeout": "60s"
    },
    "tts": {
      "endpoint": "https://tts.api.cloud.yandex.net/speech/v1/tts:synthesize",
      "voice": "alena",
      "language": "ru-RU",
      "format": "oggopus",
      "speed": 1.0,
      "timeout": "30s"
    },
    "system_prompt": "Ты SophyGo — весёлый, добрый и немного озорной гид по интересным местам. Тебя придумала и развивает София. Отвечай голосом живо и кратко, без markdown, списков и эмодзи. Рассказывай интересные факты, но не выдумывай: если не уверен, честно скажи. Легенды и неподтверждённые версии обозначай отдельно и не выдавай за историю. Не угадывай, где находится пользователь, если тебе этого не сообщили. Не изображай известных персонажей и реальных людей. Тон — уважительный и безопасный для детей и взрослых.",
    "session_ttl": "30m",
    "history_limit": 20,
    "max_audio_bytes": 1048576,
    "max_answer_bytes": 10485760
  },
  "log": {
    "level": "info"
  }
}
```

Пример `bot/settings/bot.json`:

```json
{
  "backend": {
    "url": "http://127.0.0.1:8081",
    "location_path": "/api/v1/guide/location",
    "timeout": "10s",
    "retries": 2,
    "retry_backoff": "300ms",
    "max_response_bytes": 1048576,
    "max_error_body_bytes": 4096
  },
  "ai": {
    "session_path": "/api/v1/ai/session",
    "session_stop_path": "/api/v1/ai/session/stop",
    "voice_path": "/api/v1/ai/voice",
    "timeout": "120s",
    "max_voice_bytes": 10485760
  },
  "telegram": {
    "update_timeout": 60
  },
  "log": {
    "level": "info"
  }
}
```

Длительности задаются строками (`5s`, `300ms`). Неизвестные поля в JSON считаются ошибкой, поэтому опечатки не игнорируются. Значения проверяются при старте: пустые адреса, неположительные таймауты и некорректные пути приводят к понятной ошибке.

### Переменные окружения

У каждого приложения свой env-файл, который загружается автоматически при старте:

- [`backend/.env.backend`](backend/.env.backend:1) — переменные backend;
- [`bot/.env.bot`](bot/.env.bot:1) — переменные бота.

Оба файла исключены из Git (`.env.*` в `.gitignore`). Заполните их нужными значениями.

`backend/.env.backend`:

```dotenv
BACKEND_ADDR=
LOG_LEVEL=
BACKEND_SETTINGS_FILE=
# Необязательно: переопределяют ai.api_key / ai.folder_id из JSON
YANDEX_API_KEY=
YANDEX_FOLDER_ID=
```

`bot/.env.bot`:

```dotenv
TELEGRAM_BOT_TOKEN=ваш_токен_от_BotFather
BACKEND_URL=
LOG_LEVEL=
BOT_SETTINGS_FILE=
```

Пустое значение означает «использовать значение из JSON». Пути к JSON-файлам тоже можно переопределить через `BACKEND_SETTINGS_FILE` и `BOT_SETTINGS_FILE`. Сами файлы `.env` можно разместить в других местах, задав `BACKEND_ENV_FILE` и `BOT_ENV_FILE`.

### Голосовой режим (SpeechKit STT → YandexGPT → TTS)

1. Пользователь отправляет `/start` — бот просит backend открыть сессию диалога (`POST /api/v1/ai/session`).
2. Пользователь записывает голосовое сообщение; бот скачивает OGG/Opus из Telegram.
3. Бот отправляет аудио в `POST /api/v1/ai/voice` (base64).
4. Backend отправляет OGG/Opus в SpeechKit STT, получает текст вопроса.
5. Текст вместе с историей диалога уходит в YandexGPT; ответ модели отправляется в SpeechKit TTS с `format=oggopus`.
6. Backend возвращает боту готовый OGG/Opus, бот отправляет голосовой ответ. Если аудио не пришло — отправляет текст.

Конвертации нет: Telegram, SpeechKit STT и SpeechKit TTS работают с OGG/Opus. `ffmpeg` не требуется.

Сессия хранит историю диалога (system prompt + последние сообщения) в памяти backend; закрывается по `ai.session_ttl` или через `POST /api/v1/ai/session/stop`.

Ключи Yandex задаются в `ai.api_key` и `ai.folder_id` файла [`backend/settings/backend.json`](backend/settings/backend.json:1). Переменные `YANDEX_API_KEY` / `YANDEX_FOLDER_ID` имеют приоритет. Если ключей нет, backend продолжает работать, а AI-endpoint'ы отвечают `503`.

Системный промпт гида задаётся в `ai.system_prompt`. Ограничение синхронного SpeechKit STT — до 1 МБ аудио (примерно 30 секунд); это учтено в `ai.max_audio_bytes`.

Приоритет значений: реальное окружение → env-файл → JSON. `TELEGRAM_BOT_TOKEN` читается только из окружения и никогда не пишется в JSON или логи. Токен получайте через [@BotFather](https://t.me/BotFather). Не коммитьте `.env.*`.

## Запуск

Каждое приложение само загружает свой env-файл, поэтому `source` не нужен. Запускайте команды из корня репозитория.

Запустите Backend:

```bash
go run ./backend/cmd/backend
```

Запустите Bot (в отдельном терминале):

```bash
go run ./bot/cmd/bot
```

Оба приложения запускаются независимо.

## Проверка

```bash
gofmt -l .
go test ./...
go vet ./...
go build ./...
```

Для проверки health check Backend:

```bash
curl http://127.0.0.1:8081/health
```

## API Backend

### `GET /health`

Проверка доступности сервиса.

```bash
curl http://127.0.0.1:8081/health
```

Ответ:

```json
{"status":"ok"}
```

### `POST /api/v1/guide/location`

Принимает координаты пользователя от Telegram-бота.

```bash
curl -X POST http://127.0.0.1:8081/api/v1/guide/location \
  -H 'Content-Type: application/json' \
  -d '{"latitude":52.5163,"longitude":13.3777}'
```

Ответ:

```json
{"message":"Привет, путешественник! Я получил твои координаты. Скоро расскажу, что интересного находится вокруг."}
```

На текущем этапе это **заглушка**: endpoint валидирует координаты (широта −90…90, долгота −180…180), но не ищет достопримечательности и не вызывает LLM. Некорректный JSON, неизвестные поля и координаты вне диапазона возвращают `400`; слишком большое тело — `413`; неверный метод — `405`.

### `POST /api/v1/ai/session`

Открывает сессию диалога пользователя (вызывается ботом по `/start`).

```bash
curl -X POST http://127.0.0.1:8081/api/v1/ai/session \
  -H 'Content-Type: application/json' \
  -d '{"user_id":123}'
```

Ответ: `{"status":"ok"}`. Если Yandex недоступен — `502`, если AI отключён или нет ключей — `503`.

### `POST /api/v1/ai/session/stop`

Закрывает сессию пользователя. Тело и ответ те же.

### `POST /api/v1/ai/voice`

Голосовой вопрос → голосовой ответ.

```bash
curl -X POST http://127.0.0.1:8081/api/v1/ai/voice \
  -H 'Content-Type: application/json' \
  -d '{"user_id":123,"audio":"<base64 OGG/Opus>","format":"ogg"}'
```

Ответ:

```json
{
  "transcript": "распознанный вопрос",
  "text": "текст ответа",
  "audio": "<base64 OGG/Opus>",
  "format": "ogg"
}
```

Ошибки: `400` — некорректный JSON/аудио, `413` — слишком большое аудио, `502` — ошибка Yandex, `503` — AI отключён, `504` — таймаут.

## Этапы разработки

См. `plans/SophyGo_START.md`.
