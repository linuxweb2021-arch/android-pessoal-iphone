#!/usr/bin/env bash
set -euo pipefail

base=/root/Appdata/android
password=$(sed -n 's/^Senha inicial: //p' "$base/secrets/initial-credentials.txt")
login_payload=$(jq -n --arg username vitorfulll --arg password "$password" '{username:$username,password:$password}')
login_response=$(curl --fail --silent -H 'Content-Type: application/json' -d "$login_payload" http://127.0.0.1:18080/v1/login)
access_token=$(jq -r .accessToken <<<"$login_response")

session_response=$(curl --fail --silent -H "Authorization: Bearer $access_token" -H 'Content-Type: application/json' -d '{"quality":"economy"}' http://127.0.0.1:18080/v1/sessions)
session_id=$(jq -r .id <<<"$session_response")
cleanup() {
  curl --silent -X DELETE -H "Authorization: Bearer $access_token" "http://127.0.0.1:18080/v1/sessions/$session_id" >/dev/null || true
}
trap cleanup EXIT
webrtc_ready=no
for _ in $(seq 1 20); do
  if ss -lun | grep -q ':25000 '; then
    webrtc_ready=yes
    break
  fi
  sleep 1
done

gateway_state=$(systemctl is-active android-gateway.service)
scrcpy_pid=$(adb -s 127.0.0.1:5556 shell pidof app_process 2>/dev/null | tr -d '\r' || true)
echo "api_login=ok"
echo "session_create=ok"
echo "gateway=$gateway_state"
echo "webrtc_udp=$webrtc_ready"
[[ "$webrtc_ready" == yes ]]
if [[ -n "$scrcpy_pid" ]]; then
  echo "scrcpy_server=running"
else
  echo "scrcpy_server=not_detected"
  exit 1
fi

curl --fail --silent -X DELETE -H "Authorization: Bearer $access_token" "http://127.0.0.1:18080/v1/sessions/$session_id" >/dev/null
trap - EXIT
echo "session_revoke=ok"
