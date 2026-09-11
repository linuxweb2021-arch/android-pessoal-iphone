#!/usr/bin/env bash
set -euo pipefail

readonly RELEASE=20250330
readonly ARCHIVE="MindTheGapps-14.0.0-x86_64-${RELEASE}.zip"
readonly EXPECTED_SHA256=ea6738b4908c1290447f610f365e84d46ca894c9a65eef90b83030d769990d7f
readonly URL="https://github.com/s1204IT/MindTheGappsBuilder/releases/download/${RELEASE}/${ARCHIVE}"
readonly BASE_IMAGE="redroid/redroid@sha256:68ae34dfbdb1000687d89691f12b9e17daa1b5c3ce528ada7e9d007090daab86"
readonly TARGET_IMAGE="local/redroid:14-gapps-minimal-${RELEASE}"

work_dir="${1:-$(pwd)/.build/redroid-gapps}"
mkdir -p "$work_dir"
cd "$work_dir"

if [[ ! -f "$ARCHIVE" ]]; then
  curl --fail --location --retry 3 --output "$ARCHIVE" "$URL"
fi

actual_sha256=$(sha256sum "$ARCHIVE" | awk '{print $1}')
if [[ "$actual_sha256" != "$EXPECTED_SHA256" ]]; then
  echo "MindTheGapps checksum mismatch" >&2
  exit 1
fi

rm -rf context
mkdir context
unzip -q "$ARCHIVE" 'system/*' -d context

# These optional packages add almost 400 MB and are unnecessary for a personal
# Play Store runtime. SetupWizard also assumes physical Wi-Fi and crashes on
# ReDroid's virtual Ethernet, so login is performed directly from Play Store.
rm -rf \
  context/system/product/priv-app/Velvet \
  context/system/product/priv-app/Wellbeing \
  context/system/product/priv-app/GoogleRestore \
  context/system/product/priv-app/AndroidAutoStub \
  context/system/system_ext/priv-app/SetupWizard \
  context/system/system_ext/priv-app/GoogleFeedback \
  context/system/product/overlay/GmsSetupWizardOverlay.apk

find context/system -type d -exec chmod 0755 {} +
find context/system -type f -exec chmod 0644 {} +

cat > context/Dockerfile <<EOF
FROM ${BASE_IMAGE}
COPY --chown=0:0 system/ /system/
EOF

docker build --pull=false --tag "$TARGET_IMAGE" context
docker image inspect "$TARGET_IMAGE" --format 'Built {{.Id}} ({{.Size}} bytes)'
