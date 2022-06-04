#!/bin/sh

echo "Copying unit file"
cp /home/websitewatcher/websitewatcher.service /etc/systemd/system/websitewatcher.service
cp /home/websitewatcher/websitewatcher.timer /etc/systemd/system/websitewatcher.timer
echo "reloading systemctl"
systemctl daemon-reload
echo "enabling service"
systemctl enable websitewatcher.timer
systemctl start websitewatcher.timer
systemctl start websitewatcher.service
systemctl status websitewatcher.service
systemctl status websitewatcher.timer
systemctl list-timers --all
