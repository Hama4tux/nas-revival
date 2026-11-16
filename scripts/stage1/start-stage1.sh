#!/bin/sh
set -eu
exec sh "$(dirname "$0")/control-stage1.sh" start "$@"
