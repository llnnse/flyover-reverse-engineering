#!/bin/bash

# Generates config.json from files on macOS

set -Eeuo pipefail

rmurl=""
# Try from ResourceManifest.pbd cache if present
if [ -f "$HOME/Library/Caches/GeoServices/ResourceManifest.pbd" ]; then
  rmurl="$(strings "$HOME/Library/Caches/GeoServices/ResourceManifest.pbd" | grep "https://gspe35-ssl.ls.apple.com/geo_manifest" | head -n1 | tr -d '"\r\n' || true)"
fi

# Try from com.apple.GEO.plist
if [ -z "$rmurl" ] && [ -f "$HOME/Library/Preferences/com.apple.GEO.plist" ]; then
  rmurl="$(plutil -p "$HOME/Library/Preferences/com.apple.GEO.plist" 2>/dev/null | grep GEOLastResourceManifestURL | awk '{print $3}' | cut -d '"' -f 2 || true)"
fi

# Try from GEOConfigStore.db
if [ -z "$rmurl" ] && [ -f "$HOME/Library/Containers/com.apple.geod/Data/Library/Caches/com.apple.geod/GEOConfigStore.db" ]; then
  tmp="$(mktemp -d)"
  cp "$HOME/Library/Containers/com.apple.geod/Data/Library/Caches/com.apple.geod/GEOConfigStore.db"* "$tmp/" 2>/dev/null || true
  if [ -f "$tmp/GEOConfigStore.db" ]; then
    rmurl="$(sqlite3 "$tmp/GEOConfigStore.db" "select value from defaults where key = 'GEOLastResourceManifestURL';" 2>/dev/null || true)"
  fi
  rm -rf "$tmp"
fi

# Fallback: construct standard dynamic config URL from system info
if [ -z "$rmurl" ]; then
  hw="$(sysctl -n hw.model 2>/dev/null || echo "MacBookPro18,1")"
  os_ver="$(sw_vers -productVersion 2>/dev/null || echo "14.0")"
  os_bld="$(sw_vers -buildVersion 2>/dev/null || echo "23A344")"
  rmurl="https://gspe35-ssl.ls.apple.com/geo_manifest/dynamic/config?application=geod&application_version=1&country_code=US&hardware=${hw}&os=osx&os_build=${os_bld}&os_version=${os_ver}"
fi

# Extract token
token=""
if [ -f /System/Library/PrivateFrameworks/GeoServices.framework/GeoServices ]; then
  token="$(strings /System/Library/PrivateFrameworks/GeoServices.framework/GeoServices 2>/dev/null | grep 'xyzABC' -A1 | tail -n1 || true)"
fi

if [ -z "$token" ]; then
  token="4cjLaD4jGRwlQ9U"
fi

echo "{
  \"resourceManifestURL\": \"$rmurl\",
  \"tokenP1\": \"$token\"
}"
