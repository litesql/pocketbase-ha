#!/usr/bin/env bash
# Create a record on node1 and wait until node2 and node3 can read it.
set -euo pipefail

NODE1="${NODE1_URL:-http://127.0.0.1:8090}"
NODE2="${NODE2_URL:-http://127.0.0.1:8091}"
NODE3="${NODE3_URL:-http://127.0.0.1:8092}"
EMAIL="${PB_SUPERUSER_EMAIL:-test@example.com}"
PASS="${PB_SUPERUSER_PASS:-1234567890}"
COLLECTION="cluster_probe"

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
			{\"name\":\"title\",\"type\":\"text\",\"required\":true}
		]}" >/dev/null
fi

TITLE="probe-$(date +%s)"
create_resp="$(curl -fsS -X POST "${NODE1}/api/collections/${COLLECTION}/records" \
	-H "Authorization: ${TOKEN}" \
	-H 'Content-Type: application/json' \
	-d "{\"title\":\"${TITLE}\"}")"
RECORD_ID="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"$create_resp")"

wait_record() {
	local url="$1"
	local i
	for i in $(seq 1 80); do
		rec="$(curl -fsS -H "Authorization: ${TOKEN}" \
			"${url}/api/collections/${COLLECTION}/records/${RECORD_ID}" 2>/dev/null || true)"
		if python3 -c 'import json,sys; d=json.loads(sys.argv[1]); raise SystemExit(0 if d.get("title")==sys.argv[2] else 1)' \
			"$rec" "$TITLE" 2>/dev/null; then
			return 0
		fi
		sleep 0.25
	done
	echo "verify-cluster: ${url} never saw record ${RECORD_ID} title=${TITLE}" >&2
	return 1
}

wait_record "$NODE2"
wait_record "$NODE3"
echo "cluster replication ok (${RECORD_ID})"
