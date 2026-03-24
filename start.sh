#!/bin/bash

# Pindah ke direktori proyek Jeda
cd "$(dirname "$0")"

echo "================================================="
echo "        🚀 STARTING JEDA SELF-HOSTED 🚀        "
echo "================================================="

# 1. Pastikan Docker Redis menyala
echo "1. Menyalakan Redis Container..."
docker-compose up -d redis

# Tunggu sejenak agar Redis siap
sleep 2

# 2. Build Aplikasi untuk Performa Cepat
echo "2. Mengompilasi engine Go..."
go build -o bin/api ./cmd/api
go build -o bin/worker ./cmd/worker

# Hentikan worker & api yang mungkin masih berjalan sebelumnya (Cleanup)
pkill -f bin/worker || true
pkill -f bin/api || true

# 3. Nyalakan Worker ke Balik Layar (Background)
echo "3. Menjalankan Asynq Worker di Latar Belakang..."
./bin/worker &
WORKER_PID=$!

# Tangkap sinyal CTRL+C untuk mematikan Worker saat API ditutup
trap "echo -e '\nMematikan Jeda Worker (Graceful Shutdown)...'; kill $WORKER_PID; exit 0" SIGINT SIGTERM

# 4. Nyalakan API
echo "4. Menjalankan HTTP API & Dashboard Dashboard..."
./bin/api
