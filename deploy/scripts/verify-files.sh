#!/usr/bin/env bash
# Upload a file on node1, GET it from node2. Local-disk fixtures also check replace.
set -euo pipefail

NODE1="${NODE1_URL:-http://127.0.0.1:8090}"
NODE2="${NODE2_URL:-http://127.0.0.1:8091}"
EMAIL="${PB_SUPERUSER_EMAIL:-test@example.com}"
PASS="${PB_SUPERUSER_PASS:-1234567890}"
COLLECTION="filecache_probe"
CHECK_REPLACE="${CHECK_REPLACE:-true}"

auth() {
	curl -fsS -X POST "$1/api/collections/_superusers/auth-with-password" \
		-H 'Content-Type: application/json' \
		-d "{\"identity\":\"${EMAIL}\",\"password\":\"${PASS}\"}" \
		| python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])'
}

TOKEN="$(auth "$NODE1")"

code="$(curl -s -o /dev/null -w '%{http_code}' \
	-H "Authorization: ${TOKEN}" \
	"${NODE1}/api/collections/${COLLECTION}")"
if [[ "$code" != "200" ]]; then
	curl -fsS -X POST "${NODE1}/api/collections" \
		-H "Authorization: ${TOKEN}" \
		-H 'Content-Type: application/json' \
		-d "{\"name\":\"${COLLECTION}\",\"type\":\"base\",\"fields\":[
			{\"name\":\"title\",\"type\":\"text\",\"required\":true},
			{\"name\":\"doc\",\"type\":\"file\",\"maxSelect\":1,\"maxSize\":1048576}
		]}" >/dev/null
fi

TMP1="$(mktemp)"
TMP2="$(mktemp)"
trap 'rm -f "$TMP1" "$TMP2"' EXIT
printf 'hello-filecache-1' >"$TMP1"
printf 'hello-filecache-2' >"$TMP2"

create_resp="$(curl -fsS -X POST "${NODE1}/api/collections/${COLLECTION}/records" \
	-H "Authorization: ${TOKEN}" \
	-F "title=probe-1" \
	-F "doc=@${TMP1};type=text/plain")"

RECORD_ID="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"$create_resp")"
FILENAME="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["doc"])' <<<"$create_resp")"

for _ in $(seq 1 80); do
	if curl -fsS -H "Authorization: ${TOKEN}" \
		"${NODE2}/api/collections/${COLLECTION}/records/${RECORD_ID}" >/dev/null 2>&1; then
		break
	fi
	sleep 0.25
done

body="$(curl -fsS "${NODE2}/api/files/${COLLECTION}/${RECORD_ID}/${FILENAME}")"
if [[ "$body" != "hello-filecache-1" ]]; then
	echo "replica GET after create: expected hello-filecache-1, got: $body" >&2
	exit 1
fi

if [[ "$CHECK_REPLACE" != "true" ]]; then
	echo "files replica GET ok (create)"
	exit 0
fi

update_resp="$(curl -fsS -X PATCH "${NODE1}/api/collections/${COLLECTION}/records/${RECORD_ID}" \
	-H "Authorization: ${TOKEN}" \
	-F "doc=@${TMP2};type=text/plain")"
NEW_FILENAME="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["doc"])' <<<"$update_resp")"

for _ in $(seq 1 80); do
	rec="$(curl -fsS -H "Authorization: ${TOKEN}" \
		"${NODE2}/api/collections/${COLLECTION}/records/${RECORD_ID}" 2>/dev/null || true)"
	if python3 -c 'import json,sys; d=json.loads(sys.argv[1]); raise SystemExit(0 if d.get("doc")==sys.argv[2] else 1)' \
		"$rec" "$NEW_FILENAME" 2>/dev/null; then
		break
	fi
	sleep 0.25
done

body="$(curl -fsS "${NODE2}/api/files/${COLLECTION}/${RECORD_ID}/${NEW_FILENAME}")"
if [[ "$body" != "hello-filecache-2" ]]; then
	echo "replica GET after update: expected hello-filecache-2, got: $body" >&2
	exit 1
fi

old_code="$(curl -s -o /dev/null -w '%{http_code}' "${NODE2}/api/files/${COLLECTION}/${RECORD_ID}/${FILENAME}")"
if [[ "$old_code" == "200" ]]; then
	echo "replica still serving replaced file ${FILENAME}" >&2
	exit 1
fi

echo "files replica GET ok (create+update)"
