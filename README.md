# AiRouter

Приватная прокси-панель для управления доступом к AI API (xynera.vip). Выдаёт собственные API-ключи пользователям и проксирует запросы к upstream, совместимо с OpenAI SDK, Anthropic SDK и Gemini SDK.

## Стек

| Компонент | Технология |
|-----------|-----------|
| Backend   | Go 1.22, chi, pgx, go-redis |
| Frontend  | React + Vite + TypeScript |
| База данных | PostgreSQL 16 |
| Кэш / Rate limit | Redis 7 |
| Деплой    | Docker Compose |

---

## Быстрый старт

### 1. Клонировать репозиторий

```bash
git clone https://github.com/<your-org>/AiRouter.git
cd AiRouter
```

### 2. Создать `.env`

```bash
cp .env.example .env
```

Заполнить файл:

```env
POSTGRES_DB=airouter
POSTGRES_USER=airouter
POSTGRES_PASSWORD=your-strong-password

# Токен для входа в админ-панель
ADMIN_TOKEN=your-super-secret-admin-token

# Upstream AI API (xynera.vip)
UPSTREAM_BASE_URL=https://www.xynera.vip
UPSTREAM_API_KEY=your-xynera-api-key

# URL бэкенда, который видит браузер (для production — ваш домен)
VITE_API_URL=http://localhost:8080
```

### 3. Запустить

```bash
docker compose up -d --build
```

| Сервис    | URL                    |
|-----------|------------------------|
| Фронтенд  | http://localhost:3000  |
| Backend API | http://localhost:8080 |
| PostgreSQL | localhost:5432        |
| Redis     | localhost:6379         |

### 4. Войти в панель

Откройте http://localhost:3000 и введите значение `ADMIN_TOKEN` из `.env`.

---

## Как пользоваться

### Создать API-ключ

В панели → **API Keys** → **New Key**. После создания ключ показывается **один раз** — сохраните его.

Ключи имеют формат `ar-<48 hex chars>`.

### Использовать ключ с OpenAI SDK

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="ar-ваш-ключ"
)

r = client.chat.completions.create(
    model="gpt-5.5",
    messages=[{"role": "user", "content": "Привет!"}]
)
print(r.choices[0].message.content)
```

### Использовать ключ с Anthropic SDK

```python
import anthropic

client = anthropic.Anthropic(
    base_url="http://localhost:8080/v1",
    api_key="ar-ваш-ключ"
)

msg = client.messages.create(
    model="gpt-5.5",
    max_tokens=512,
    messages=[{"role": "user", "content": "Привет!"}]
)
print(msg.content[0].text)
```

### Поддерживаемые эндпоинты (proxy)

| Путь | Совместимость |
|------|--------------|
| `POST /v1/chat/completions` | OpenAI SDK |
| `POST /v1/messages` | Anthropic SDK |
| `GET  /v1beta/models/` | Gemini SDK |
| `/*` под `/v1` и `/v1beta` | любые другие |

---

## Admin REST API

Все эндпоинты требуют заголовок `Authorization: Bearer <ADMIN_TOKEN>`.

### Ключи

| Метод | Путь | Описание |
|-------|------|----------|
| `GET`    | `/admin/keys` | Список всех ключей |
| `POST`   | `/admin/keys` | Создать ключ |
| `DELETE` | `/admin/keys/:id` | Удалить ключ |
| `PATCH`  | `/admin/keys/:id/toggle` | Включить / отключить ключ |

**Создать ключ:**
```bash
curl -X POST http://localhost:8080/admin/keys \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "my-app", "note": "prod key", "expires_at": "2026-12-31T00:00:00Z"}'
```

Ответ:
```json
{
  "key": { "id": "...", "name": "my-app", "key_prefix": "ar-3f9e1a...", ... },
  "secret": "ar-3f9e1a..."
}
```

### Статистика и логи

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/admin/stats` | Общая статистика |
| `GET` | `/admin/stats/daily?days=7` | По дням (до 90 дней) |
| `GET` | `/admin/logs?limit=50&offset=0` | Лог запросов |

---

## Rate limiting

По умолчанию — **60 запросов в минуту** на API-ключ через Redis. При превышении возвращается `429 Too Many Requests` с заголовком `Retry-After: 60`.

---

## Production-деплой

1. Укажите реальный домен в `VITE_API_URL` и поставьте nginx/traefik перед контейнерами.
2. Смените `ADMIN_TOKEN` и `POSTGRES_PASSWORD` на сильные значения.
3. Включите HTTPS (Let's Encrypt или ваш сертификат).

---

## Структура проекта

```
AiRouter/
├── backend/
│   ├── cmd/server/main.go       # точка входа
│   ├── internal/
│   │   ├── config/              # конфигурация из env
│   │   ├── db/                  # подключение к БД и миграции
│   │   ├── handlers/            # admin HTTP handlers
│   │   ├── middleware/          # auth, rate limit
│   │   ├── models/              # структуры данных
│   │   ├── proxy/               # прокси к upstream API
│   │   └── repository/          # SQL запросы
│   ├── migrations/001_init.sql
│   └── Dockerfile
├── frontend/
│   ├── src/
│   │   ├── api/                 # axios клиент
│   │   ├── context/             # AuthContext
│   │   ├── components/          # Layout
│   │   └── pages/               # Login, Dashboard, Keys, Logs
│   ├── nginx.conf
│   └── Dockerfile
├── docker-compose.yml
├── .env.example
└── README.md
```
