# EBS Autoscale

**ebs-autoscale** maintains a filesystem across one or more ebs volumes. As the filesystem fills up, ebs-autoscale will detect the change in useage and recruit additional ebs volumes to the filesystem.

This project is inspired by: https://github.com/awslabs/amazon-ebs-autoscale

## Dependencies

**ebs-autoscale** creates files systems across ebs volumes. As there are no dedicated Go libraries to do this, ebs-autoscale must shell-out to external processes.

Currently, ebs-autoscale creates a **btrfs** file system across the target devices.

`btrfs-progs` must be installed locally before ebs-autoscale is run.

```bash
sudo yum install -y btrfs-progs
```

Future releases may support other file systems.

## Installation

**ebs-autoscale** distributes as a command-line binary.

The utility is installed in three steps:

1) **config.json** - a configuration jason must be created and made readable. By default, ebs-autoscale looks for `/etc/ebs-autoscale/ebs-autoscale.json`.
2) **initialisation** - the initial device and mount point need to be created. This process must be run with root privileges.
3) **monitor** - monitoring is a long-running process that detects changes in the filesystem usage. This process must be run with root privileges.

## Usage

### Configuration:

The following is an example of the configuration json:

```txt
{
  "logging": {                              ## An optional section to define Cloudwatch logging configuration.
    "log-group-name": "/my/log/group/name", ## The name of the CLoudwatch Log Group for sending logging
    "poll-interval": 5,                     ## The interval in seconds between sending batches of logs.
    "max-batch-size": 100,                  ## The maximum number of cloudwatch log events to buffer before sending
    "log-level": "INFO"                     ## The Log level of the logger: DEBUG|INFO|WARN|ERROR
  },
  "monitor": {
    "interval": 5,      ## The polling interval in seconds
    "threshold-pc": 50, ## The percentage usage threshold triggering volume grow event
  },
  "filesystem": {
    "path": "/mnt/ebs-autoscale",   ## The file system mount path
    "ebs-type": "gp3",              ## The ebs volume type to use (io1, io2, gp3)
    "ebs-throughput": 150,          ## The throughput value for each ebs volume(optional)
    "ebs-iops": 3000,               ## The IOPS value for each ebs volume (optional)
    "initial-size-gb": 50,          ## The size in GB of the first ebs volume
    "max-size-gb": 500,             ## The maximum, combined size in GB of the filesystem
    "ebs-max-attached-volumes": 16, ## The maximum number of allowed volumes for the instance. This should reflect the maximum allowed number of volumes defined by AWS. Currently defaults to 16
    "ebs-max-created-volumes": 5    ## The maximum number of volumes to recruit for this filesystem.
    "backend": {                    ## Filesystem backend config
      "type": "btrfs",              ## The underlying filesystem
      "fs-specific": {}             ## Underlying filesytem specific config - see below
    }
  }
}
```

#### Backends

##### Btrfs

type: btrfs
fs-specific: {}

### Initialisation

The following command recruits the first volume and initialises the file system:

```bash
sudo ebs-autoscale init --config /path/to/config.json
```

### Monitor

The following command monitors the configured filesystem and grows it when it's usage reaches the threshold defined in the config.json.
This is the command called by the systemd monitor-service module:

```bash
sudo ebs-autoscale monitor-service --config /path/to/config.json
```

#### Volume Grow Events

Volume grow events are triggered when the useage of the monitored volume exceeds `monitor.threshold-pc`.
The size of the recruited volume is caclulated from `filesystem.max-size-gb` divided by `filesystem.ebs-max-created-volumes` (less the initial volume size and count). This way the size of each additional volume can fine tuned.

### Monitoring as a Service

**ebs-autoscale** is intended to be run in two steps - initialisation and monitoring. 

By performing the init outside the service, you can use the resulting volume immediately which is useful when creating volumes as part of an instances user-data cloud-init.

Once initialised, the monitoring service will expand the volume when its configured usage threshold has been exceeded.

If initialising the service by hand, use the service defaults located within this repository.

#### For systemd:
```bash
sudo mkdir /mnt/ebs-autoscale
sudo mkdir /etc/ebs-autoscale
sudo cp service/ebs-autoscale.json /etc/ebs-autoscale/ebs-autoscale.json

## Edit the ebs-autoscale.json as you see fit.

sudo cp ebs-autoscale /usr/local/bin

## init must be run with sufficient privileges to create and mount the underlying file system.
sudo ebs-autoscale init 

## By performing the init directly you can use the volume immediately, which is useful when initialising a volume
## during user-data cloud-init.

sudo cp service/ebs-autoscale-monitor.service /etc/systemd/ebs-autoscale-monitor.service
sudo systemctl daemon-reload

sudo systemctl enable ebs-autoscale-monitor.service
sudo systemctl start ebs-autoscale-monitor.service
```

## AWS IAM Role Permissions

The ec2 instance will require the following permissions to allow ebs-autoscale to function correctly:

`allowInstanceOperations` is required to read the tags from the local instance.
It is recommended to limit which instance tags can be read by identifying the instance(s).
See the `Condition` block for an example. ebs-autoscale will copy tags from the instance to the volumes on creation.

`enableCloudwatchLoggingPutEvents` allows the utility to push cloudwatch logs to a log group. Replace `<log group arn>` with your log group.

`enableCreationOfCloudwatchStreams` allows the utility to create cloudwatch log streams. Replace `<log group arn>` with your log group.

`allowVolumeOperations` is required to create volumes.

`allowTagCreationOnVolumeCreationOnly` limits the ability of the role to create tags on volumes associated with this instance.

`allowCurrentInstanceToDeleteOwnedVolumesOnly` limits the ability of the role to delete volumes tagged by ebs-autoscale with the instance arn.

```json
[
  {
    "Sid": "enableCloudwatchLoggingPutEvents",
    "Effect": "Allow",
    "Action": [
      "logs:GetLogEvents",
      "logs:PutLogEvents"
    ],
    "Resource": "<log group arn>:*"
  },
  {
    "Sid": "enableCreationOfCloudwatchStreams",
    "Effect": "Allow",
    "Action": [
      "logs:CreateLogStream",
      "logs:DescribeLogStreams"
    ],
    "Resource": "<log group arn>:*"
  },
  {
    "Sid": "allowInstanceOperations",
    "Effect": "Allow",
    "Action": [
      "ec2:AttachVolume",
      "ec2:DescribeTags",
      "ec2:ModifyInstanceAttribute"
    ],
    "Resource": [
      "arn:aws:ec2:*:*:instance/*",
      "arn:aws:ec2:*:*:volume/*"
    ],
    "Condition": {
      "StringEquals": { "ec2:ResourceTag/<some-identifying-tag>": "<some-value>" }
    }
  },
  {
    "Sid": "allowVolumeOperations",
    "Effect": "Allow",
    "Action": [
      "ec2:CreateVolume",
      "ec2:DescribeVolumes",
      "ec2:DescribeVolumeAttribute"
    ],
    "Resource": [
      "arn:aws:ec2:*:*:volume/*"
    ]
  },
  {
    "Sid": "allowTagCreationOnVolumeCreationOnly",
    "Effect": "Allow",
    "Action": [
      "ec2:CreateTags"
    ],
    "Resource": [
      "arn:aws:ec2:*:*:volume/*"
    ],
    "Condition": {
      "StringEquals": { "ec2:CreateAction": "CreateVolume" }
    }
 },
 {
    "Sid": "allowCurrentInstanceToDeleteOwnedVolumesOnly",
    "Effect": "Allow",
    "Action": [
      "ec2:DeleteVolume",
      "ec2:DetachVolume"
    ],
    "Resource": [
      "arn:aws:ec2:*:*:volume/*"
    ],
    "Condition": {
      "StringEquals": { "ec2:ResourceTag/source-instance-arn": "${ec2:SourceInstanceARN}" }
    }
 }
]
```
