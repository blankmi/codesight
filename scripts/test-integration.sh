#!/usr/bin/env bash
set -euo pipefail

IMAGE="${CODESIGHT_MILVUS_IMAGE:-milvusdb/milvus:v2.6.4}"
CONTAINER_NAME="${CODESIGHT_MILVUS_CONTAINER:-codesight-it-milvus-$$}"
KEEP_CONTAINER="${CODESIGHT_KEEP_MILVUS_CONTAINER:-0}"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker CLI not found" >&2
  exit 1
fi

if ! docker info >/dev/null 2>&1; then
  echo "docker daemon is not available (check permissions/runtime)" >&2
  exit 1
fi

cleanup() {
  if [[ "$KEEP_CONTAINER" == "1" ]]; then
    echo "Keeping container $CONTAINER_NAME (CODESIGHT_KEEP_MILVUS_CONTAINER=1)"
    return
  fi
  docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "Starting Milvus container: $CONTAINER_NAME"
docker run -d \
  --name "$CONTAINER_NAME" \
  -p 127.0.0.1::19530 \
  -p 127.0.0.1::9091 \
  -e DEPLOY_MODE=STANDALONE \
  -e ETCD_USE_EMBED=true \
  -e ETCD_DATA_DIR=/var/lib/milvus/etcd \
  -e COMMON_STORAGETYPE=local \
  "$IMAGE" milvus run standalone >/dev/null

PORT_LINE="$(docker port "$CONTAINER_NAME" 19530/tcp | head -n1)"
if [[ ! "$PORT_LINE" =~ :([0-9]+)$ ]]; then
  echo "unable to parse mapped Milvus port from: $PORT_LINE" >&2
  docker logs --tail 100 "$CONTAINER_NAME" >&2 || true
  exit 1
fi

MILVUS_PORT="${BASH_REMATCH[1]}"
export CODESIGHT_INTEGRATION_DB_ADDRESS="127.0.0.1:${MILVUS_PORT}"

echo "Milvus gRPC address: $CODESIGHT_INTEGRATION_DB_ADDRESS"
echo "Running integration test..."
set +e
go test -tags=integration ./pkg -run TestIndexerAndSearcher_MilvusIntegration -v
status=$?
set -e

if [[ $status -ne 0 ]]; then
  echo "Integration test failed. Recent Milvus logs:" >&2
  docker logs --tail 200 "$CONTAINER_NAME" >&2 || true
fi

exit $status
