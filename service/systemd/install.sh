#!/bin/sh

# enable the service and start
systemctl daemon-reload
systemctl enable ebs-autoscale-monitor.service

## It is currently recommended to start the process after init has been performed
# systemctl start ebs-autoscale-monitor.service
