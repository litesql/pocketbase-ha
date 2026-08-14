#!/bin/sh
# Create the RustFS/S3 bucket used by PocketBase file storage.
set -eu

ENDPOINT="${RUSTFS_ENDPOINT:-http://rustfs:9000}"
BUCKET="${S3_BUCKET:-pb-files}"

export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-rustfsadmin}"
export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-rustfsadmin}"
export AWS_DEFAULT_REGION="${AWS_DEFAULT_REGION:-us-east-1}"
export AWS_EC2_METADATA_DISABLED=true

i=0
while [ "$i" -lt 60 ]; do
	if aws --endpoint-url "$ENDPOINT" s3 mb "s3://${BUCKET}" 2>/dev/null; then
		echo "create-bucket: created s3://${BUCKET}"
		exit 0
	fi
	if aws --endpoint-url "$ENDPOINT" s3 ls "s3://${BUCKET}" >/dev/null 2>&1; then
		echo "create-bucket: s3://${BUCKET} already exists"
		exit 0
	fi
	i=$((i + 1))
	sleep 2
done

echo "create-bucket: failed to create s3://${BUCKET} at ${ENDPOINT}" >&2
exit 1
