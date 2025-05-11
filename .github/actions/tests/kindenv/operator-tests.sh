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