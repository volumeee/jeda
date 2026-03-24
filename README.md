<p align="center">
  <div align="center">
    <h1 style="font-size: 3em;">⚡ Jeda</h1>
    <p><b>Self-Hosted Cloud Task Scheduler & Cron Jobs</b></p>
  </div>
</p>

> **The ultimate self-hosted alternative to Upstash QStash.**
>
> Jeda is a lightning-fast, zero-config cloud task scheduler and cron jobs dispatcher. Delegate delayed webhook executions and timezone-aware cron jobs via a simple REST API, backed purely by Redis.

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
docker pull bagose/jeda-scheduler:latest
```

**Running with Docker Compose:**
Create a `docker-compose.yml`:
```yaml
version: '3.8'
services:
  jeda:
    image: bagose/jeda-scheduler:latest
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

### Auto-Provisioning Keys

You don't need to configure secrets manually. Upon the first startup, Jeda detects missing keys, **auto-generates** them, saves them to `.env`, and prints them to the terminal:

```
🔑 API KEY (Kirim Task)    : jd_api_7x89ashdas...
🔑 SIGNING KEY (Verifikasi): jd_sig_x834hsdf7sdff...
```

---

## 📖 API Reference

Every endpoint requires the header: `Authorization: Bearer <JEDA_API_KEY>`.

### 1. `POST /v1/tasks` — Submit a Task
Schedule a delayed webhook or an immediate execution.

**Request Body:**
| Field | Type | Description |
|---|---|---|
| `destination` | `string` | The target URL to hit. |
| `body` | `object` | The JSON payload to send to the destination. |
| `delay` | `string` | (Optional) Time to delay execution. e.g. `30s`, `2h`, `7d`. |
| `cron` | `string` | (Optional) Cron expression. Upgrades task to a periodic schedule. |
| `timezone` | `string` | (Optional) Cron timezone execution. e.g. `Asia/Jakarta`. |
| `env` | `string` | (Optional) Separates traffic (e.g., `production`, `staging`). |
| `retries` | `integer`| (Optional) Override default max retries. |

**Advanced Headers:**
| Header | Purpose |
|---|---|
| `Jeda-Forward-[Key]` | Strips the prefix and forwards the header. Example: `Jeda-Forward-Authorization: Bearer token`. |
| `Jeda-Deduplication-Id` | Prevents double executions. Exact IDs are deduplicated strictly. |
| `Jeda-Failure-Callback` | URL notified if the task permanently fails exhaust retries. |
| `Jeda-Queue-Group` | Enforces **Strict FIFO Queues**. Tasks with same group ID execute sequentially. |

---

### 2. `GET /v1/tasks` — List Active Tasks
Retrieve a list of tasks currently scheduled or pending.

**Query Parameters:**
- `?queue=` (Optional) Filter by queue type (`default`, `scheduler`, `fifo-*`). Default is `all`.
- `?env=` (Optional) Filter by environment metadata.

---

### 3. `POST /v1/tasks/{id}/update` — Modifying Tasks
Modify an existing queued task or cron schedule.

**Query Parameters:**
- `?queue=` (Required) Specify which queue the task belongs to (e.g. `?queue=scheduler` for cron tasks or `?queue=default` for webhook tasks).

**Request Body:**
Pass any fields you wish to update (`destination`, `body`, `cron`, `delay`, `env`).

---

### 4. `POST /v1/tasks/{id}/force` — Fire Now
Force a queued task or a cron schedule to execute immediately, bypassing the timer. If it's a one-off delayed task, it will be executed and removed from the queue immediately.

**Query Parameters:**
- `?queue=` (Required) e.g., `?queue=scheduler` or `?queue=default`.

---

### 5. `DELETE /v1/tasks/{id}` — Delete a Task
Permamently remove a task/cron schedule from the queue so it no longer triggers.

**Query Parameters:**
- `?queue=` (Required) Specifying the queue is necessary for Asynq deletion.

---

### 6. `POST /v1/test-webhook` — Ping Test
Perform an immediate, synchronous HTTP outbound request sent by Jeda to verify URL connectivity without queueing it.

**Request Body:**
```json
{
  "destination": "https://api.example.com",
  "body": { "hello": "world" }
}
```

---

### 7. Queue Management
- `POST /v1/queue/pause` : Suspends all worker activity. No webhooks will be fired. Tasks will safely backlog.
- `POST /v1/queue/resume` : Resumes processing of backlogged tasks.

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
