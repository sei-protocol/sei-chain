#!/bin/bash

SOURCE_ROOT=$(git rev-parse --show-toplevel)
cd "${SOURCE_ROOT}" || exit
GIT_HASH=$(git rev-parse --short "$GITHUB_SHA")
BUNDLE_FILE="build/seid_bundle.zip"
COMPONENT_NAME="seid"

while IFS="" read -r REGION || [ -n "$REGION" ]
do
  export AWS_DEFAULT_REGION=$REGION
    BUCKET_URI="s3://sei-artifacts-$REGION/$REPO_NAME/$COMPONENT_NAME/release/$GIT_HASH.zip"
    aws s3 cp $BUNDLE_FILE $BUCKET_URI
done < scripts/REGIONS