#!/bin/bash

ROLLBACK_COMMIT=$1

echo "Rolling back to commit $ROLLBACK_COMMIT"

COMMIT=$(echo $(seid version --long | grep commit) | awk '{ print $NF }')
if [ ${COMMIT[1]} != $1 ]
then
  echo "incorrect seid version"
  exit 1
fi

echo "Stopping seid systemctl process"
systemctl stop seid

echo "Updating Binary to $ROLLBACK_COMMIT"
git checkout $ROLLBACK_COMMIT
make install


seid version --long | head -n 4

echo "Rolling Back State"
seid rollback --hard

echo "Restarting seid"
systemctl restart seid
