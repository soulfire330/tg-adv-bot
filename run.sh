#!/usr/bin/env bash
# Запуск и вход. Использование: ./run.sh [up|login|logs]
set -euo pipefail
cd "$(dirname "$0")"

export PUID="$(id -u)" PGID="$(id -g)"

case "${1:-up}" in
  up)
    if [ ! -f .env ]; then
      cp .env.example .env
      echo "→ .env создан. Заполни TG_API_ID, TG_API_HASH, TG_PHONE, TG_RECIPIENT и LLM_*, потом запусти снова."
      exit 1
    fi
    [ -f keywords.txt ] || cp keywords.txt.example keywords.txt
    mkdir -p data
    docker compose up -d --build
    if [ ! -f data/session.json ]; then
      echo
      echo "Сессии нет — нужен одноразовый вход с кодом из Telegram и 2FA:"
      echo "    ./run.sh login"
      echo
    fi
    docker compose logs -f --tail=20
    ;;
  login)
    docker compose run --rm app login
    ;;
  logs)
    docker compose logs -f --tail=50
    ;;
  *)
    echo "использование: ./run.sh [up|login|logs]" >&2
    exit 1
    ;;
esac
