#!/bin/sh
set -e
if [ "$1" != "remove" ] && [ "$1" != "purge" ]; then
  exit 0
fi
systemctl disable photo-sorter || true
systemctl stop photo-sorter    || true
