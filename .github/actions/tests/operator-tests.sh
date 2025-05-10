#!/usr/bin/env bash

# mock microservice APP1 creating todays log file
TIMESTAMP=$(date +%Y%m%d)

LOG_DIR="/mnt/disks/logs/App1"
LOG_FILE="$LOG_DIR/App1-$TIMESTAMP.log"
sudo mkdir -p "$LOG_DIR"
sudo chmod 777 "$LOG_DIR"
sudo touch  "$LOG_FILE"
sudo chmod 666 "$LOG_FILE"

echo "test daily log" | sudo tee -a "$LOG_FILE" > /dev/null

# retrieve the log from S3 bucket and verify the content