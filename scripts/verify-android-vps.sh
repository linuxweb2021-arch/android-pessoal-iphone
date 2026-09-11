#!/usr/bin/env bash
set -euo pipefail

serial=127.0.0.1:5556
marker=/data/local/tmp/android-pessoal-persistence-test
printf verified | adb -s "$serial" shell "cat > $marker"
docker restart android-runtime >/dev/null

for _ in $(seq 1 90); do
  adb connect "$serial" >/dev/null 2>&1 || true
  if [[ "$(adb -s "$serial" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" == 1 ]]; then
    break
  fi
  sleep 1
done

[[ "$(adb -s "$serial" shell getprop sys.boot_completed | tr -d '\r')" == 1 ]]
[[ "$(adb -s "$serial" shell cat "$marker" | tr -d '\r')" == verified ]]
echo "android_boot=ok"
echo "android_persistence=ok"
echo "android_version=$(adb -s "$serial" shell getprop ro.build.version.release | tr -d '\r')"
echo "android_abi=$(adb -s "$serial" shell getprop ro.product.cpu.abilist | tr -d '\r')"
echo "android_renderer=$(adb -s "$serial" shell getprop ro.hardware.egl | tr -d '\r')/$(adb -s "$serial" shell getprop ro.hardware.vulkan | tr -d '\r')"

packages=$(adb -s "$serial" shell pm list packages)
if grep -q 'com.android.vending' <<<"$packages"; then echo "play_store=present"; else echo "play_store=absent"; fi
if grep -q 'com.google.android.gms' <<<"$packages"; then echo "play_services=present"; else echo "play_services=absent"; fi

codec_files=$(adb -s "$serial" shell find /vendor/etc /system/etc -maxdepth 2 -name 'media_codecs*.xml' 2>/dev/null | tr -d '\r')
if [[ -n "$codec_files" ]]; then
  echo "codec_configuration=present"
else
  echo "codec_configuration=absent"
fi

docker ps --filter name=android-runtime --filter name=android-proxy
docker stats --no-stream android-runtime
