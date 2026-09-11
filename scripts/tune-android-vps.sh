#!/usr/bin/env bash
set -euo pipefail

serial=${ANDROID_ADB_SERIAL:-127.0.0.1:5556}

adb connect "$serial" >/dev/null
for _ in $(seq 1 60); do
  if [[ "$(adb -s "$serial" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" == "1" ]]; then
    break
  fi
  sleep 1
done

[[ "$(adb -s "$serial" shell getprop sys.boot_completed | tr -d '\r')" == "1" ]]
adb -s "$serial" shell settings put global window_animation_scale 0
adb -s "$serial" shell settings put global transition_animation_scale 0
adb -s "$serial" shell settings put global animator_duration_scale 0
echo "android_tuning=ok"
