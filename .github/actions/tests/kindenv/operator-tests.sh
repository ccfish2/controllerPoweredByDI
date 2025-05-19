#!/usr/bin/env bash

# 
# mock microservice APP1 creating todays log file
TIMESTAMP=$(date +%Y%m%d)

LOG_DIR="/tmp/kind-node1-logs/App1"
sudo mkdir -p "$LOG_DIR"
sudo chmod 777 "$LOG_DIR"
LOG_FILE="$LOG_DIR/App1-$TIMESTAMP.log"
sudo touch  "$LOG_FILE"
sudo chmod 666 "$LOG_FILE"
echo "node1 app1 test daily log" | sudo tee -a "$LOG_FILE" > /dev/null

LOG_DIR="/tmp/kind-node1-logs/App2"
sudo mkdir -p "$LOG_DIR"
sudo chmod 777 "$LOG_DIR"
LOG_FILE="$LOG_DIR/App2-$TIMESTAMP.log"
sudo touch  "$LOG_FILE"
sudo chmod 666 "$LOG_FILE"
echo "node1 app2 test daily log" | sudo tee -a "$LOG_FILE" > /dev/null

LOG_DIR="/tmp/kind-node2-logs/App1"
sudo mkdir -p "$LOG_DIR"
sudo chmod 777 "$LOG_DIR"
LOG_FILE="$LOG_DIR/App1-$TIMESTAMP.log"
sudo touch  "$LOG_FILE"
sudo chmod 666 "$LOG_FILE"
echo "node2 app1 test daily log" | sudo tee -a "$LOG_FILE" > /dev/null

LOG_DIR="/tmp/kind-node2-logs/App2"
sudo mkdir -p "$LOG_DIR"
sudo chmod 777 "$LOG_DIR"
LOG_FILE="$LOG_DIR/App2-$TIMESTAMP.log"
sudo touch  "$LOG_FILE"
sudo chmod 666 "$LOG_FILE"
echo "node2 app2 test daily log" | sudo tee -a "$LOG_FILE" > /dev/null

sleep 3m

echo "mock log uploading periodically every 1 minute, checking file content"

check_log_file() {
    local LOG_DIR="$1"
    local LOG_FILE="$LOG_DIR/App1-$TIMESTAMP.log"
    
    echo "Checking content within $LOG_FILE"

    if [[ -f "$LOG_FILE" ]]; then
        if [[ ! -s "$LOG_FILE" ]]; then
            echo "Log file $LOG_FILE is empty."
            exit 1
        fi

        local last_line
        last_line=$(tail -n 1 "$LOG_FILE")
        echo "$last_line"
        
        if [[ "$last_line" == *"Mocking Uploading to S3"* ]]; then
            echo "$last_line"
        else
            echo "Log file does not contain the expected content."
            
        fi
    else
        echo "Log file does not exist."
        exit 1
    fi
}

# Replace this with the actual timestamp value if not already set
# export TIMESTAMP="2025-05-18-12-00"  # example value
check_log_file "/tmp/kind-node1-logs/App1"
check_log_file "/tmp/kind-node2-logs/App1"