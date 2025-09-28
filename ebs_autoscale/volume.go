package ebs_autoscale

import (
	"context"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/BobTheTerrible/ebs-autoscale/ebs_autoscale/filesystem"
	"math"
	"os"
	"strings"
	"time"
)

type Volume struct {
	Host               Ec2Host
	Fs                 filesystem.FileSystem
	Id                 string
	EbsType            string
	ThroughPut         *int32
	Iops               *int32
	InitialSizeGb      int32
	MaxLogicalSizeGb   int32
	MaxAttachedVolumes int32
	MaxCreatedVolumes  int32
	ManagedVolumes     []types.Volume
	ec2Client          ec2.Client
}

var (
	volumeTypes map[string]any
)

func init() {
	volumeTypes = map[string]any{
		"io1": types.VolumeTypeIo1,
		"io2": types.VolumeTypeIo2,
		"gp3": types.VolumeTypeGp3,
	}
}

func NewVolume(ctx context.Context, host Ec2Host, fs filesystem.FileSystem, cfg VolumeCfg) (*Volume, error) {

	// Get the region from the Host instance. Use this for subsequent aws calls
	awsConfig, err := config.LoadDefaultConfig(ctx, config.WithDefaultRegion(host.Region))
	if err != nil {
		return nil, err
	}

	ec2Client := ec2.NewFromConfig(awsConfig)

	// Get a list of all attached volumes
	attachedVolumesOutput, err := ec2Client.DescribeVolumes(ctx, &ec2.DescribeVolumesInput{
		Filters: []types.Filter{
			{
				Name: aws.String("attachment.instance-id"),
				Values: []string{
					host.InstanceId,
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	attachedVolumes := attachedVolumesOutput.Volumes

	// Find the volumes created by this config
	ebsAutoscaleId := Md5String(fs.GetMountPoint())
	managedVolumes := make([]types.Volume, 0)
	for _, v := range attachedVolumes {
		for _, t := range v.Tags {
			if *t.Key == "ebs-autoscale-id" && *t.Value == ebsAutoscaleId {
				managedVolumes = append(managedVolumes, v)
				break
			}
		}
	}

	v := Volume{
		Host:               host,
		Fs:                 fs,
		Id:                 ebsAutoscaleId,
		EbsType:            cfg.EbsType,
		ThroughPut:         cfg.EbsThroughput,
		Iops:               cfg.EbsIops,
		InitialSizeGb:      cfg.InitialSizeGb,  // Set initial size from config
		MaxLogicalSizeGb:   cfg.MaxSizeGb,
		MaxAttachedVolumes: cfg.EbsMaxAttachedVolumes,
		MaxCreatedVolumes:  cfg.EbsMaxCreatedVolumes,
		ManagedVolumes:     managedVolumes,
		ec2Client:          *ec2Client,
	}

	return &v, nil
}

// managedVolumeSizeGb returns the total size of the filesystem volumes in Gb
func (v Volume) managedVolumeSizeGb() int32 {

	totalVolumeSize := int32(0)
	for _, mv := range v.ManagedVolumes {
		totalVolumeSize += *mv.Size
	}
	return totalVolumeSize
}

// TotalUsagePercent returns the usage as a percentage
func (v Volume) TotalUsagePercent() (float32, error) {

	usagePercent := float32(0)

	total, used, _, err := v.Fs.Stat()
	if err != nil {
		return usagePercent, err
	}

	if total == 0 {
		return usagePercent, nil
	}
	usagePercent = (float32(used) / float32(total)) * 100
	return usagePercent, nil
}

// CreateVolume creates the volume and filesystem for the given configuration
func (v *Volume) CreateVolume(ctx context.Context) error {

	device, err := v.createAndAttachEbsVolume(ctx, v.InitialSizeGb)
	if err != nil {
		return err
	}
	err = v.Fs.CreateFileSystem(*device)
	if err != nil {
		return err
	}

	return nil
}

// GrowVolume grows the volume by the given amount
func (v *Volume) GrowVolume(ctx context.Context) error {
	// Calculate the total available size to grow
	sizeIncreasePerVolume, err := v.calculateSizeIncreasePerVolume()
	if err != nil {
		return err
	}

	// Attach a new ebs volume by the calculated size increase
	device, err := v.createAndAttachEbsVolume(ctx, sizeIncreasePerVolume)
	if err != nil {
		return err
	}

	// After attaching, expand the filesystem across the new device
	err = v.Fs.GrowFileSystem(*device)
	if err != nil {
		return err
	}

	return nil
}

// calculateSizeIncreasePerVolume calculates the increase in size per volume, taking into account the max size and the initial volume.
func (v *Volume) calculateSizeIncreasePerVolume() (int32, error) {
	difference := v.MaxLogicalSizeGb - v.InitialSizeGb
	if difference <= 0 {
		return 0, fmt.Errorf("calculateSizeIncreasePerVolume: Cannot grow, the volume size is already at or beyond max size")
	}

	// Calculate the size increase per volume, rounding down to the nearest GB
	// Subtract 1 from MaxCreatedVolumes to account for the initial volume already created
	sizeIncreasePerVolume := int32(math.Floor(float64(difference) / float64(v.MaxCreatedVolumes-1)))
	return sizeIncreasePerVolume, nil
}

// getNextLogicalDevice attempts to determine the next Dev name for a given range of values i.e. /dev/xvda to /dev/xvdz
func (v Volume) getNextLogicalDevice() (*string, error) {

	for i := 'a'; i <= 'z'; i++ {

		//use /dev/xvdb* device names to avoid contention for /dev/sd* and /dev/xvda names

		device := "/dev/xvdb" + string(i)

		b, err := isAvailable(device)
		if err != nil {
			return nil, err
		}
		if b {
			return &device, nil
		}
	}
	return nil, errors.New("Volume.getNextLogicalDevice: Could not determine the next logical device")
}

// isAvailable determines if the given path exists on the file system. In the context of this class, it is used to
// determine if a device has been mounted to a path in /dev.
func isAvailable(path string) (bool, error) {

	_, err := os.Stat(path)
	if err == nil {
		// The path exists and therefore in use
		return false, nil
	}

	if errors.Is(err, os.ErrNotExist) {
		// path does *not* exist
		return true, nil
	}

	return false, fmt.Errorf("isAvailable: unexpected error from os.Stat: %w", err)
}

// createAndAttachEbsVolume will create and attach an ebs volume of the given size and expand the filesystem across it
func (v *Volume) createAndAttachEbsVolume(ctx context.Context, sizeGb int32) (*string, error) {

	volSize := v.managedVolumeSizeGb()
	if volSize > v.MaxLogicalSizeGb {
		return nil, fmt.Errorf("createAndAttachEbsVolume: MaxLogicalSizeGb exceeded: max:%dGb observed:%dGb", v.MaxLogicalSizeGb, volSize)
	}

	if int32(len(v.ManagedVolumes)) == v.MaxCreatedVolumes {
		return nil, fmt.Errorf("createAndAttachEbsVolume: MaxCreatedVolumes reached: max:%d observed:%d", v.MaxCreatedVolumes, len(v.ManagedVolumes))
	}

	// Get a list of all attached volumes - this could have changed since we last looked
	c, totalVolumes, err := v.instanceHasCapacity(ctx)
	if err != nil {
		return nil, err
	}
	if !c {
		return nil, fmt.Errorf("createAndAttachEbsVolume: MaxAttachedVolumes exceeded: max:%d observed:%d", v.MaxAttachedVolumes, totalVolumes)
	}

	device, err := v.getNextLogicalDevice()
	if err != nil {
		return nil, err
	}

	ec2Client := v.ec2Client

	vol, err := ec2Client.CreateVolume(ctx, &ec2.CreateVolumeInput{
		AvailabilityZone: aws.String(v.Host.AvailabilityZone),
		VolumeType:       volumeTypes[v.EbsType].(types.VolumeType),
		Size:             &sizeGb,
		Iops:             v.Iops,
		Throughput:       v.ThroughPut,
		Encrypted:        nil,
		TagSpecifications: []types.TagSpecification{
			{
				ResourceType: types.ResourceTypeVolume,
				Tags:         v.buildVolumeTags(time.Now),
			},
		},
	})
	if err != nil {
		return nil, err
	}

	// wait till volume is available....
	volWaiter := ec2.NewVolumeAvailableWaiter(&ec2Client)

	err = volWaiter.Wait(ctx, &ec2.DescribeVolumesInput{
		VolumeIds: []string{*vol.VolumeId},
	}, 20*time.Second)
	if err != nil {
		// there is a problem describing the new volume, clean it up
		err2 := v.removeVolume(ctx, *vol.VolumeId)
		if err2 != nil {
			return nil, errors.Join(err, err2)
		}
		return nil, err
	}

	_, err = ec2Client.AttachVolume(ctx, &ec2.AttachVolumeInput{
		Device:     device,
		InstanceId: aws.String(v.Host.InstanceId),
		VolumeId:   vol.VolumeId,
	})
	if err != nil {
		// there is a problem attaching the new volume, clean it up
		err2 := v.removeVolume(ctx, *vol.VolumeId)
		if err2 != nil {
			return nil, errors.Join(err, err2)
		}
		return nil, err
	}

	v.ManagedVolumes = append(v.ManagedVolumes, createVolumeOutputToVolume(*vol))

	// Set the volume to be deleted on termination
	_, err = ec2Client.ModifyInstanceAttribute(ctx, &ec2.ModifyInstanceAttributeInput{
		InstanceId: aws.String(v.Host.InstanceId),
		BlockDeviceMappings: []types.InstanceBlockDeviceMappingSpecification{
			{
				DeviceName: device,
				Ebs: &types.EbsInstanceBlockDeviceSpecification{
					DeleteOnTermination: aws.Bool(true),
					VolumeId:            vol.VolumeId,
				},
			},
		},
	})
	if err != nil {
		// if there is a problem marking the new volume for deletion, clean it up
		err2 := v.removeVolume(ctx, *vol.VolumeId)
		if err2 != nil {
			return nil, errors.Join(err, err2)
		}
		return nil, err
	}

	// Wait till the device is actually available in /dev....
	err = localVolAvailabilityWaiter(ctx, *device, 50*time.Second)
	if err != nil {
		return nil, err
	}

	return device, nil
}

// removeVolume removes an attached volume from the instance. This is a best effort process to be used when an error
// occurs when attaching a volume.
func (v Volume) removeVolume(ctx context.Context, volumeId string) error {

	ec2Client := v.ec2Client

	var errList []error

	_, err := ec2Client.DetachVolume(ctx, &ec2.DetachVolumeInput{
		VolumeId: aws.String(volumeId),
	})
	if err != nil {
		errList = append(errList, err)
	}

	// wait till volume is available....
	volWaiter := ec2.NewVolumeAvailableWaiter(&ec2Client)
	err = volWaiter.Wait(ctx, &ec2.DescribeVolumesInput{
		VolumeIds: []string{volumeId},
	}, 20*time.Second)
	if err != nil {
		errList = append(errList, err)
	}

	_, err = ec2Client.DeleteVolume(ctx, &ec2.DeleteVolumeInput{
		VolumeId: aws.String(volumeId),
	})
	if err != nil {
		errList = append(errList, err)
	}

	// bundle up the errors of the steps that failed
	return errors.Join(errList...)
}

// instanceHasCapacity checks to see if we have reached the maximum number of ebs volumes this instance can accept.
// Returns true if the instance has capacity and the count of observed ebs volumes
func (v Volume) instanceHasCapacity(ctx context.Context) (bool, int, error) {

	attachedVolumesOutput, err := v.ec2Client.DescribeVolumes(ctx, &ec2.DescribeVolumesInput{
		Filters: []types.Filter{
			{
				Name: aws.String("attachment.instance-id"),
				Values: []string{
					v.Host.InstanceId,
				},
			},
		},
	})
	if err != nil {
		return false, 0, err
	}

	count := len(attachedVolumesOutput.Volumes)
	if int32(count) > v.MaxAttachedVolumes {
		return false, count, nil
	}
	return true, count, nil
}

// localVolAvailabilityWaiter for the given device, will wait until either the device is attached ad appears under /dev
// or the timeoutLimit expires. If the timeout expires an error is thrown.
func localVolAvailabilityWaiter(ctx context.Context, device string, timeoutLimit time.Duration) error {

	ctxTimeout, timeoutCancel := context.WithTimeout(ctx, timeoutLimit)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer func() {
		ticker.Stop()
		timeoutCancel()
	}()

	for {
		select {
		case <-ticker.C:
			b, err := isAvailable(device)
			if err != nil {
				return err
			}
			if !b {
				return nil
			}
			ticker.Reset(50 * time.Millisecond)
		case <-ctxTimeout.Done():
			return fmt.Errorf("localVolAvailabilityWaiter: waiting for device: %s appears to have timed out", device)
		}
	}
}

// buildVolumeTags builds a set of volume tags for the volume
func (v Volume) buildVolumeTags(now func() time.Time) []types.Tag {

	volumeTags := []types.Tag{
		{
			Key:   aws.String("source-instance"),
			Value: aws.String(v.Host.InstanceId),
		},
		{
			Key:   aws.String("source-instance-arn"),
			Value: aws.String(v.Host.InstanceArn),
		},
		{
			Key:   aws.String("ebs-autoscale-id"),
			Value: aws.String(v.Id),
		},
		{
			Key:   aws.String("ebs-autoscale-creation-time"),
			Value: aws.String(now().String()),
		},
	}

	// AWS does not allow us to use any tags that begin with 'aws:'
	for _, t := range v.Host.Tags {
		if !strings.HasPrefix(*t.Key, "aws:") {
			volumeTags = append(volumeTags, t)
		}
	}

	return volumeTags
}

// createVolumeOutputToVolume performs a type conversion from ec2.CreateVolumeOutput to types.Volume
func createVolumeOutputToVolume(o ec2.CreateVolumeOutput) types.Volume {

	return types.Volume{
		Attachments:        o.Attachments,
		AvailabilityZone:   o.AvailabilityZone,
		CreateTime:         o.CreateTime,
		Encrypted:          o.Encrypted,
		FastRestored:       o.FastRestored,
		Iops:               o.Iops,
		KmsKeyId:           o.KmsKeyId,
		MultiAttachEnabled: o.MultiAttachEnabled,
		OutpostArn:         o.OutpostArn,
		Size:               o.Size,
		SnapshotId:         o.SnapshotId,
		SseType:            o.SseType,
		State:              o.State,
		Tags:               o.Tags,
		Throughput:         o.Throughput,
		VolumeId:           o.VolumeId,
		VolumeType:         o.VolumeType,
	}
}
