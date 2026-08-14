#!/usr/bin/env bash
# Verify replica GET /api/files after a leader upload, then after a file replace.
# Requires docker compose (node1 :8090, node2 :8091) and the default superuser.
set -euo pipefail

LEADER="${LEADER_URL:-http://127.0.0.1:8090}"
REPLICA="${REPLICA_URL:-http://127.0.0.1:8091}"
EMAIL="${PB_SUPERUSER_EMAIL:-test@example.com}"
PASS="${PB_SUPERUSER_PASS:-1234567890}"
COLLECTION="filecache_probe"

auth() {
  curl -fsS -X POST "$1/api/collections/_superusers/auth-with-password" \
    -H 'Content-Type: application/json' \
    -d "{\"identity\":\"${EMAIL}\",\"password\":\"${PASS}\"}" \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])'
}

TOKEN="$(auth "$LEADER")"

ensure_collection() {
  local code
  code="$(curl -s -o /dev/null -w '%{http_code}' \
    -H "Authorization: ${TOKEN}" \
    "$LEADER/api/collections/${COLLECTION}")"
  if [[ "$code" == "200" ]]; then
    return
  fi
  curl -fsS -X POST "$LEADER/api/collections" \
    -H "Authorization: ${TOKEN}" \
    -H 'Content-Type: application/json' \
    -d "{\"name\":\"${COLLECTION}\",\"type\":\"base\",\"fields\":[
      {\"name\":\"title\",\"type\":\"text\",\"required\":true},
      {\"name\":\"doc\",\"type\":\"file\",\"maxSelect\":1,\"maxSize\":1048576}
    ]}" >/dev/null
}

ensure_collection

TMP1="$(mktemp)"
TMP2="$(mktemp)"
trap 'rm -f "$TMP1" "$TMP2"' EXIT
printf 'hello-filecache-1' >"$TMP1"
printf 'hello-filecache-2' >"$TMP2"

create_resp="$(curl -fsS -X POST "$LEADER/api/collections/${COLLECTION}/records" \
  -H "Authorization: ${TOKEN}" \
  -F "title=probe-1" \
  -F "doc=@${TMP1};type=text/plain")"

RECORD_ID="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"$create_resp")"
FILENAME="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["doc"])' <<<"$create_resp")"

# wait briefly for SQL replication to the replica
for _ in $(seq 1 40); do
  if curl -fsS -H "Authorization: ${TOKEN}" \
    "$REPLICA/api/collections/${COLLECTION}/records/${RECORD_ID}" >/dev/null 2>&1; then
    break
  fi
  sleep 0.25
done

body="$(curl -fsS "$REPLICA/api/files/${COLLECTION}/${RECORD_ID}/${FILENAME}")"
if [[ "$body" != "hello-filecache-1" ]]; then
  echo "replica GET after create: expected hello-filecache-1, got: $body" >&2
  exit 1
fi

update_resp="$(curl -fsS -X PATCH "$LEADER/api/collections/${COLLECTION}/records/${RECORD_ID}" \
  -H "Authorization: ${TOKEN}" \
  -F "doc=@${TMP2};type=text/plain")"
NEW_FILENAME="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["doc"])' <<<"$update_resp")"

for _ in $(seq 1 40); do
  rec="$(curl -fsS -H "Authorization: ${TOKEN}" \
    "$REPLICA/api/collections/${COLLECTION}/records/${RECORD_ID}" 2>/dev/null || true)"
  if python3 -c 'import json,sys; d=json.loads(sys.argv[1]); raise SystemExit(0 if d.get("doc")==sys.argv[2] else 1)' "$rec" "$NEW_FILENAME" 2>/dev/null; then
    break
  fi
  sleep 0.25
done

body="$(curl -fsS "$REPLICA/api/files/${COLLECTION}/${RECORD_ID}/${NEW_FILENAME}")"
if [[ "$body" != "hello-filecache-2" ]]; then
  echo "replica GET after update: expected hello-filecache-2, got: $body" >&2
  exit 1
fi

old_code="$(curl -s -o /dev/null -w '%{http_code}' "$REPLICA/api/files/${COLLECTION}/${RECORD_ID}/${FILENAME}")"
if [[ "$old_code" == "200" ]]; then
  echo "replica still serving replaced file ${FILENAME}" >&2
  exit 1
fi

echo "filecache replica proxy ok (create+update)"
