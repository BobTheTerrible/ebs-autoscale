#!/bin/sh

systemctl stop ebs-autoscale-monitor.service
systemctl disable ebs-autoscale-monitor.service

## Currently assumes goreleaser is removing this file..
# uninstall systemd service
#rm /usr/lib/systemd/system/ebs-autoscale-monitor.service

# update daemon config
systemctl daemon-reload
