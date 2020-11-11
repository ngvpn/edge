#!/bin/bash
if [ "$#" -ne 1 ]; then
    echo "usage: ibmcloud.sh app-name"
    exit 1
fi
cat <<EOF >manifest.yml
---
applications:
- name: $1
  random-route: true
  memory: 128M
EOF