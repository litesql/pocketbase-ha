#!/bin/sh
# PATCH PocketBase settings on node1. Always sets trustedProxy.
# Set ENABLE_S3=true to also enable S3 (RustFS) file storage.
set -eu

PB_URL="${PB_URL:-http://node1:8090}"
EMAIL="${PB_SUPERUSER_EMAIL:-test@example.com}"
PASS="${PB_SUPERUSER_PASS:-1234567890}"
ENABLE_S3="${ENABLE_S3:-false}"
S3_BUCKET="${S3_BUCKET:-pb-files}"
S3_REGION="${S3_REGION:-us-east-1}"
S3_ENDPOINT="${S3_ENDPOINT:-http://rustfs:9000}"
S3_ACCESS_KEY="${S3_ACCESS_KEY:-rustfsadmin}"
S3_SECRET_KEY="${S3_SECRET_KEY:-rustfsadmin}"

TOKEN=""
i=0
while [ "$i" -lt 60 ]; do
	RESP="$(curl -sS -X POST "${PB_URL}/api/collections/_superusers/auth-with-password" \
		-H 'Content-Type: application/json' \
		-d "{\"identity\":\"${EMAIL}\",\"password\":\"${PASS}\"}" || true)"
	TOKEN="$(printf '%s' "$RESP" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')"
	if [ -n "$TOKEN" ]; then
		break
	fi
	i=$((i + 1))
	sleep 2
done

if [ -z "$TOKEN" ]; then
	echo "bootstrap-settings: failed to authenticate as superuser at ${PB_URL}" >&2
	exit 1
fi

if [ "$ENABLE_S3" = "true" ]; then
	BODY="$(printf '%s' "{\"trustedProxy\":{\"headers\":[\"X-Forwarded-For\"],\"useLeftmostIP\":false},\"s3\":{\"enabled\":true,\"bucket\":\"${S3_BUCKET}\",\"region\":\"${S3_REGION}\",\"endpoint\":\"${S3_ENDPOINT}\",\"accessKey\":\"${S3_ACCESS_KEY}\",\"secret\":\"${S3_SECRET_KEY}\",\"forcePathStyle\":true}}")"
else
	BODY='{"trustedProxy":{"headers":["X-Forwarded-For"],"useLeftmostIP":false}}'
fi

curl -fsS -X PATCH "${PB_URL}/api/settings" \
	-H "Authorization: ${TOKEN}" \
	-H 'Content-Type: application/json' \
	-d "$BODY" >/dev/null

if [ "$ENABLE_S3" = "true" ]; then
	echo "bootstrap-settings: trustedProxy + S3 (${S3_ENDPOINT} ${S3_BUCKET}) applied"
else
	echo "bootstrap-settings: trustedProxy applied"
fi
