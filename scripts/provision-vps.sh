#!/usr/bin/env bash
set -euo pipefail

base=/root/Appdata/android
umask 077
mkdir -p "$base/bin" "$base/data/auth" "$base/secrets" "$base/reports"

if [[ ! -f "$base/secrets/api.env" ]]; then
  jwt_secret=$(openssl rand -hex 32)
  executor_token=$(openssl rand -hex 32)
  initial_password=$(openssl rand -base64 18 | tr '+/' 'AZ')
  cat > "$base/secrets/api.env" <<EOF
ANDROID_LISTEN_ADDRESS=127.0.0.1:18080
ANDROID_DATABASE_PATH=$base/data/auth/auth.db
ANDROID_JWT_SECRET=$jwt_secret
ANDROID_EXECUTOR_TOKEN=$executor_token
ANDROID_BOOTSTRAP_USER=vitorfulll
ANDROID_BOOTSTRAP_PASSWORD=$initial_password
ANDROID_ACCESS_TTL_MINUTES=10
ANDROID_REFRESH_TTL_HOURS=720
EOF
  cat > "$base/secrets/initial-credentials.txt" <<EOF
Usuário: vitorfulll
Senha inicial: $initial_password
EOF
fi

executor_token=$(sed -n 's/^ANDROID_EXECUTOR_TOKEN=//p' "$base/secrets/api.env")
cat > "$base/secrets/gateway.env" <<EOF
ANDROID_API_BASE_URL=http://127.0.0.1:18080
ANDROID_EXECUTOR_TOKEN=$executor_token
ANDROID_ADB_PATH=/usr/bin/adb
ANDROID_ADB_SERIAL=127.0.0.1:5556
ANDROID_SCRCPY_SERVER_PATH=$base/bin/scrcpy-server-v3.3.4
ANDROID_SCRCPY_PORT=27183
ANDROID_MAX_SIZE=1280
ANDROID_MAX_FPS=60
ANDROID_VIDEO_BITRATE=6000000
ANDROID_ICE_SERVERS_JSON=[{"urls":["stun:stun.cloudflare.com:3478"]}]
ANDROID_ICE_UDP_PORT=25000
EOF
chmod 600 "$base/secrets/"*

install -m 0644 "$base/runtime/android-api.service" /etc/systemd/system/android-api.service
install -m 0644 "$base/runtime/android-gateway.service" /etc/systemd/system/android-gateway.service
systemctl daemon-reload
systemctl enable --now android-api.service

for _ in $(seq 1 20); do
  if curl --fail --silent http://127.0.0.1:18080/healthz >/dev/null; then
    break
  fi
  sleep 1
done
curl --fail --silent http://127.0.0.1:18080/healthz >/dev/null

# The clear-text bootstrap password is no longer required after the first user
# was written as an Argon2id hash to SQLite.
sed -i '/^ANDROID_BOOTSTRAP_PASSWORD=/d' "$base/secrets/api.env"
systemctl restart android-api.service
systemctl enable --now android-gateway.service
