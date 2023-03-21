#!/bin/bash

ROLLBACK_COMMIT=$(echo $1 | xargs)

echo "Rolling back to commit $ROLLBACK_COMMIT"

COMMIT_INFO=$(seid version --long | grep commit)
COMMIT=$(echo $COMMIT_INFO | awk '{ print $NF }' | xargs)

if [ ${COMMIT} != $1 ]
then
  echo "incorrect seid version, make sure you check out the right commit and run make install"
  exit 1
fi

echo "Stopping seid systemctl process"
systemctl stop seid

seid version --long | head -n 4

echo "Rolling Back State"
seid rollback --hard

echo "Restarting seid"
systemctl restart seid
