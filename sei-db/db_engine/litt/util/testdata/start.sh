#!/bin/bash

# Start SSH daemon in background
/usr/sbin/sshd -D &
SSHD_PID=$!

# Self-destruct after 5 minutes (300 seconds)
(
  sleep 300
  echo "SSH test container self-destructing after 5 minutes..."
  kill $SSHD_PID
  exit 0
) &

# Wait for SSH daemon to finish
wait $SSHD_PID