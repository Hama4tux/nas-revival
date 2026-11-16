#!/bin/sh
set -eu
exec python3 "$BR2_EXTERNAL_NAS_REVIVAL_STAGE1_PATH/../../scripts/stage1/finalize.py" "$1"
