#!/usr/bin/env bash
# Запуск и вход. Использование: ./run.sh [up|login|logs]
set -euo pipefail
cd "$(dirname "$0")"

# Под sudo compose должен работать от твоего uid, а не от root:
# иначе ./data окажется root-овым и контейнер не сможет писать в него.
export PUID="${SUDO_UID:-$(id -u)}"
export PGID="${SUDO_GID:-$(id -g)}"

own() {
  if [ "$(id -u)" -eq 0 ]; then chown -R "$PUID:$PGID" "$@"; fi
}

case "${1:-up}" in
  up)
    if [ ! -f .env ]; then
      cp .env.example .env
      own .env
      echo "→ .env создан. Заполни TG_API_ID, TG_API_HASH, TG_PHONE, TG_RECIPIENT и LLM_*, потом запусти снова."
      exit 1
    fi
    if [ ! -f keywords.txt ]; then
      cp keywords.txt.example keywords.txt
      own keywords.txt
    fi
    mkdir -p data
    own data
    # Без --build: собирает только если образа нет, иначе берёт готовый
    # (например, залитый с другой машины через docker save | docker load).
    docker compose up -d
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
