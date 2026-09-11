#!/usr/bin/env bash
set -euo pipefail

app="build/device/Build/Products/Release-iphoneos/AndroidPessoal.app"
binary="$app/AndroidPessoal"
test -d "$app"
test -f "$binary"
file "$binary" | tee /tmp/android-pessoal-file.txt
grep -q "arm64" /tmp/android-pessoal-file.txt

rm -rf dist build/package
mkdir -p dist build/package/Payload
cp -R "$app" build/package/Payload/
(
  cd build/package
  ditto -c -k --sequesterRsrc --keepParent Payload ../../dist/PNHX.ipa
)
shasum -a 256 dist/PNHX.ipa > dist/PNHX.ipa.sha256

bundle_id=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$app/Info.plist")
minimum_ios=$(/usr/libexec/PlistBuddy -c 'Print :MinimumOSVersion' "$app/Info.plist")
version=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$app/Info.plist")
sha=$(awk '{print $1}' dist/PNHX.ipa.sha256)
size=$(stat -f '%z' dist/PNHX.ipa)
frameworks=$(find "$app/Frameworks" -maxdepth 1 -type d -name '*.framework' -print 2>/dev/null | sed 's#.*/##' | sort | python3 -c 'import json,sys; print(json.dumps([x.strip() for x in sys.stdin if x.strip()]))')
cat > dist/manifest.json <<JSON
{
  "artifact": "PNHX.ipa",
  "signed": false,
  "bundleId": "$bundle_id",
  "version": "$version",
  "minimumIOS": "$minimum_ios",
  "deviceArchitecture": "arm64",
  "sha256": "$sha",
  "bytes": $size,
  "embeddedFrameworks": $frameworks,
  "webrtcPackage": "stasel/WebRTC 152.0.0"
}
JSON
unzip -l dist/PNHX.ipa
cat dist/manifest.json
