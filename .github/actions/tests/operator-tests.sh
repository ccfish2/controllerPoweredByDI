#!/usr/bin/env bash

# configure one microservice and its log through configmap
sudo mkdir -p /mnt/disks/logs/App1
sudo chmod 777 /mnt/disks/logs/App1
sudo touch  /mnt/disks/logs/App1/App1-20250501.log
sudo chmod 777 /mnt/disks/logs/App1/App1-20250501.log
echo "test daily log" >> /mnt/disks/logs/App1/App1-20250502.log

# retrieve the log from S3 bucket and verify the content