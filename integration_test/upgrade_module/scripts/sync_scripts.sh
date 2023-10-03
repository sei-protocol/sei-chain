#!/bin/bash

for i in {0..3}; do
  docker cp integration_test/upgrade_module/scripts/. sei-node-$i:/usr/bin
  echo "Copied files to sei-node-$i"
done