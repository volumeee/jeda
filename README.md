<p align="center">
  <div align="center">
    <h1 style="font-size: 3em;">⚡ Jeda</h1>
    <p><b>Self-Hosted Message Broker & Task Scheduler</b></p>
  </div>
</p>

> **The ultimate self-hosted alternative to Upstash QStash.**
>
> Jeda is a lightning-fast, zero-config message broker and task scheduler. Delegate delayed webhook executions and timezone-aware cron jobs via a simple REST API, backed purely by Redis.

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Redis](https://img.shields.io/badge/Redis-7.0+-DC382D?logo=redis&logoColor=white)](https://redis.io)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker&logoColor=white)](https://docker.com)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

---

## ⚡ Architecture Overview

Jeda is designed to be **100% Plug & Play** for single-tenant or internal team usage. No complex SQL databases or heavy dependencies—just a single statically linked Go binary and Redis.

| Component | Description |
|---|---|
| **API Server** | Receives tasks, handles authentication, and powers the Admin UI. |
| **Worker** | The engine that guarantees reliable delivery, retries, and cron executions. |
| **Redis** | The only state dependency. Handles queue storage and persistence. |
| **Dashboard** | Embedded Vue.js UI for real-time monitoring and task management. |

---

## 🚀 Quick Start

### Docker (Recommended)

**Pull from Docker Hub:**
```bash
docker pull bagose/jeda:latest
```

**Running with Docker Compose:**
Create a `docker-compose.yml`:
```yaml
version: '3.8'
services:
  jeda:
    image: bagose/jeda:latest
    ports:
      - "3001:3001"
    volumes:
      - ./.env:/app/.env # Store keys persistently
    depends_on:
      - redis
  redis:
    image: redis:7-alpine
    command: redis-server --appendonly yes
    volumes:
      - redisdata:/data
volumes:
  redisdata:
```

```bash
docker-compose up -d
```
The API & Dashboard will be automatically available at `http://localhost:3001`.

### Local Development

```bash
# Clone and install dependencies
git clone https://github.com/bagose/jeda.git
cd jeda
go mod download

# Start Redis locally
docker-compose up -d redis

# Run the complete application (API + Worker wrapper)
bash start.sh
```

### Auto-Provisioning Keys

You don't need to configure secrets manually. Upon the first startup, Jeda detects missing keys, **auto-generates** them, saves them to `.env`, and prints them to the terminal:

```
🔑 API KEY (Kirim Task)    : jd_api_7x89ashdas...
🔑 SIGNING KEY (Verifikasi): jd_sig_x834hsdf7sdff...
```

---

## 📖 API Reference

### 1. `POST /v1/tasks` — Submit a Task

Schedule a delayed webhook or an immediate execution.

#### Request Body
| Field | Type | Description |
|---|---|---|
| `destination` | `string` | The target URL to hit. |
| `body` | `object` | The JSON payload to send to the destination. |
| `delay` | `string` | (Optional) Time to delay execution. e.g. `30s`, `2h`, `7d`. |
| `cron` | `string` | (Optional) Cron expression. Upgrades task to a periodic schedule. |
| `timezone` | `string` | (Optional) Executed based on local time. e.g. `Asia/Jakarta`. |
| `retries` | `integer`| (Optional) Override default max retries (default: 3). |

#### Advanced Headers
Jeda mirrors QStash's advanced header manipulation:

| Header | Purpose |
|---|---|
| `Jeda-Forward-[Key]` | Strips the prefix and forwards the header. Example: `Jeda-Forward-Authorization: Bearer token` becomes `Authorization: Bearer token` at the destination. |
| `Jeda-Deduplication-Id` | Prevents double executions. Exact IDs are deduplicated strictly. |
| `Jeda-Failure-Callback` | Provide a webhook URL. If the task fails permanently, Jeda notifies this callback. |
| `Jeda-Queue-Group` | Enforces **Strict FIFO Queues**. Tasks with the same group ID execute sequentially. |
| `Jeda-Env` | Routes tasks to `staging`, `production`, etc. Keeps the dashboard logically separated! |

#### Examples

**Delayed Task (Fire-and-forget in 2 days)**
```bash
curl -X POST http://localhost:3001/v1/tasks \
  -H "Authorization: Bearer jd_api_xxxxx" \
  -H "Content-Type: application/json" \
  -H "Jeda-Forward-X-My-Header: CustomValue" \
  -d '{
    "destination": "https://api.example.com/invoice/process",
    "body": { "invoice_id": "INV-123" },
    "delay": "48h"
  }'
```

**Timezone-Aware Cron Task**
```bash
curl -X POST http://localhost:3001/v1/tasks \
  -H "Authorization: Bearer jd_api_xxxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "destination": "https://api.example.com/daily-report",
    "cron": "0 8 * * *",
    "timezone": "Asia/Jakarta",
    "body": { "event": "generate_report" }
  }'
```

---

## 🔒 Security: Webhook Signatures

How do you know an incoming request is actually from your Jeda instance? 

Just like QStash or Stripe, Jeda calculates an **HMAC SHA-256 Hash** of the exact payload body using your `JEDA_SIGNING_KEY`. This is attached to every outgoing request in the headers:

```http
Jeda-Signature: t=161110023,v1=a7fb99...
```

Your receiving server simply hashes the raw body with the same signing key to verify authenticity and prevent malicious actors from triggering your endpoints.

---

## 📊 Premium Visual Dashboard

Access the beautiful, responsive, and glassmorphism-styled dashboard at `/ui`.
*(Requires you to enter your `JEDA_API_KEY` for access).*

**Features:**
- 📈 **Live Monitoring:** Real-time metrics tracking Pending, Active, Success, and Dead-Letter Queue (DLQ) task volumes. No refresh needed (Powered by SSE).
- ✏️ **Full Task Management (CRUD):** 
  - Add tasks via beautiful forms with auto-calculating Cron translations (e.g., `"At 08:00 AM"`).
  - Edit payloads or reschedule pending tasks directly.
  - Delete stuck tasks or purge DLQ effortlessly.
- ⚡ **Fire Test & Force Run:** Test webhook endpoints interactively inside the dashboard. Jeda acts like Postman, showing you exactly what HTTP Status and JSON response the target server returns.
- 🛑 **Emergency Pause Button:** Global kill-switch to pause all outgoing webhooks instantly if your downstream services enter maintenance. 

---

## ⚙️ Enterprise-Grade Reliability

Even as a minimal single-binary app, Jeda enforces strict safety protocols:
1. **HTTP Client Timeouts:** Strict 10-second boundaries. Prevents hanging requests from causing memory leaks.
2. **Rate Limiting:** Protects your Redis instance from infinite-loop DDOS mistakes.
3. **Dead Letter Queue (DLQ):** Tasks that exhaust their Exponential Backoff retries are shelved safely in the DLQ for manual inspection in the Dashboard.
4. **Graceful Shutdown:** Intercepts `SIGTERM`/`SIGINT`. Wait up to 15 seconds for active webhooks to finish flighting before killing the docker container—**Zero Ghost Tasks**.

---

## 📄 License

MIT License — see [LICENSE](LICENSE) for details.

---

<p align="center">
  <strong>Built with ❤️ for hassle-free scheduling.</strong>
</p>
