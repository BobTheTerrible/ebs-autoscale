package ebs_autoscale

import (
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/google/go-cmp/cmp"
	"gotest.tools/assert"
	"testing"
	"time"
)

var defaultVolume = Volume{
	Host:               Ec2Host{},
	Fs:                 nil,
	Id:                 "",
	EbsType:            "",
	ThroughPut:         nil,
	Iops:               nil,
	MaxLogicalSizeGb:   0,
	MaxAttachedVolumes: 0,
	MaxCreatedVolumes:  0,
	ManagedVolumes:     nil,
	ec2Client:          ec2.Client{},
}

var defaultEbsVolume = types.Volume{
	Attachments:        nil,
	AvailabilityZone:   nil,
	CreateTime:         nil,
	Encrypted:          nil,
	FastRestored:       nil,
	Iops:               nil,
	KmsKeyId:           nil,
	MultiAttachEnabled: nil,
	OutpostArn:         nil,
	Size:               nil,
	SnapshotId:         nil,
	SseType:            "",
	State:              "",
	Tags:               nil,
	Throughput:         nil,
	VolumeId:           nil,
	VolumeType:         "",
}

type mockFS struct {
	Size       *uint64
	Used       *uint64
	Free       *uint64
	MountPoint *string
	Err        error
}

func (t mockFS) Stat() (uint64, uint64, uint64, error) {
	return *t.Size, *t.Used, *t.Free, t.Err
}

func (t mockFS) CreateFileSystem(device string) error {
	return t.Err
}

func (t mockFS) GrowFileSystem(device string) error {
	return t.Err
}

func (t mockFS) GetMountPoint() string {
	return *t.MountPoint
}

type TestManagedVolumeSizeGbInputs struct {
	Name     string
	Volume   Volume
	Expected int32
}

func TestManagedVolumeSizeGb(t *testing.T) {
	tests := []TestManagedVolumeSizeGbInputs{
		{
			Name:     "One Managed Volume",
			Expected: 51,
			Volume: func(volume Volume) Volume {
				vol1 := defaultEbsVolume
				vol1.Size = aws.Int32(51)
				volume.ManagedVolumes = []types.Volume{
					vol1,
				}
				return volume
			}(defaultVolume),
		},
		{
			Name:     "Two Managed Volumes",
			Expected: 61,
			Volume: func(volume Volume) Volume {
				vol1 := defaultEbsVolume
				vol1.Size = aws.Int32(51)
				vol2 := defaultEbsVolume
				vol2.Size = aws.Int32(10)
				volume.ManagedVolumes = []types.Volume{
					vol1, vol2,
				}
				return volume
			}(defaultVolume),
		},
		{
			Name:     "No Managed Volumes",
			Expected: 0,
			Volume: func(volume Volume) Volume {
				volume.ManagedVolumes = []types.Volume{}
				return volume
			}(defaultVolume),
		},
	}

	for _, i := range tests {

		got := i.Volume.managedVolumeSizeGb()

		if got != i.Expected {
			t.Errorf("managedVolumeSizeGb(%s) Expected: %d Got: %d", i.Name, i.Expected, got)
		}

	}
}

type TestTotalUsagePercentInputs struct {
	Name     string
	Volume   Volume
	Expected float32
	Error    bool
}

func TestTotalUsagePercent(t *testing.T) {

	tests := []TestTotalUsagePercentInputs{
		{
			Name: "50% full",
			Volume: func(volume Volume) Volume {
				volume.Fs = mockFS{
					Size: aws.Uint64(200),
					Used: aws.Uint64(100),
					Free: aws.Uint64(100),
					Err:  nil,
				}
				return volume
			}(defaultVolume),
			Expected: 50,
			Error:    false,
		},
		{
			Name: "100% full",
			Volume: func(volume Volume) Volume {
				volume.Fs = mockFS{
					Size: aws.Uint64(200),
					Used: aws.Uint64(200),
					Free: aws.Uint64(0),
					Err:  nil,
				}
				return volume
			}(defaultVolume),
			Expected: 100,
			Error:    false,
		},
		{
			Name: "0% full",
			Volume: func(volume Volume) Volume {
				volume.Fs = mockFS{
					Size: aws.Uint64(200),
					Used: aws.Uint64(0),
					Free: aws.Uint64(200),
					Err:  nil,
				}
				return volume
			}(defaultVolume),
			Expected: 0,
			Error:    false,
		},
		{
			Name: "Fs Throws Error",
			Volume: func(volume Volume) Volume {
				volume.Fs = mockFS{
					Size: aws.Uint64(200),
					Used: aws.Uint64(0),
					Free: aws.Uint64(200),
					Err:  fmt.Errorf("mock error"),
				}
				return volume
			}(defaultVolume),
			Expected: 0,
			Error:    true,
		},
	}

	for _, i := range tests {

		got, err := i.Volume.TotalUsagePercent()

		if (err == nil) == i.Error {
			t.Errorf("managedVolumeSizeGb(%s) Returned an unxpected error: %s", i.Name, err)
		}
		if got != i.Expected {
			t.Errorf("managedVolumeSizeGb(%s) Expected: %f Got: %f", i.Name, i.Expected, got)
		}

	}

}

type TestBuildVolumeTagsInputs struct {
	Name     string
	Volume   Volume
	Expected []types.Tag
}

func TestBuildVolumeTags(t *testing.T) {

	actualNow := time.Now()
	now := func() time.Time {
		return actualNow
	}

	tests := []TestBuildVolumeTagsInputs{
		{
			Name: "Expected tags from Volume",
			Expected: []types.Tag{
				{
					Key:   aws.String("source-instance"),
					Value: aws.String("bob"),
				},
				{
					Key:   aws.String("source-instance-arn"),
					Value: aws.String("arn:bob"),
				},
				{
					Key:   aws.String("ebs-autoscale-id"),
					Value: aws.String("vol_id"),
				},
				{
					Key:   aws.String("ebs-autoscale-creation-time"),
					Value: aws.String(actualNow.String()),
				},
				{
					Key:   aws.String("HostName"),
					Value: aws.String("Mock Host Name 1"),
				},
				{
					Key:   aws.String("HostLabel"),
					Value: aws.String("Mock Host label 1"),
				},
			},
			Volume: func(volume Volume) Volume {
				volume.Host.InstanceId = "bob"
				volume.Host.InstanceArn = "arn:bob"
				volume.Id = "vol_id"
				volume.Host.Tags = []types.Tag{
					{
						Key:   aws.String("HostName"),
						Value: aws.String("Mock Host Name 1"),
					},
					{
						Key:   aws.String("aws:HostLabel"),
						Value: aws.String("This should be excluded because 'aws:' tags are not allowed"),
					},
					{
						Key:   aws.String("HostLabel"),
						Value: aws.String("Mock Host label 1"),
					},
				}
				return volume
			}(defaultVolume),
		},
	}

	for _, i := range tests {
		assert.DeepEqual(t, i.Volume.buildVolumeTags(now), i.Expected, cmp.AllowUnexported(types.Tag{}))
	}

}

type TestCalculateSizeIncreasePerVolumeInputs struct {
	Name     string
	Volume   Volume
	Expected int32
	Error    bool
}

func TestCalculateSizeIncreasePerVolume(t *testing.T) {

	tests := []TestCalculateSizeIncreasePerVolumeInputs{
		{
			Name: "Valid size increase per volume",
			Volume: func(volume Volume) Volume {
				volume.InitialSizeGb = 50
				volume.MaxLogicalSizeGb = 200
				volume.MaxCreatedVolumes = 3
				return volume
			}(defaultVolume),
			Expected: 75, // (200 - 50) / (3 - 1) = 75
			Error:    false,
		},
		{
			Name: "Max size already reached",
			Volume: func(volume Volume) Volume {
				volume.InitialSizeGb = 200
				volume.MaxLogicalSizeGb = 200
				volume.MaxCreatedVolumes = 3
				return volume
			}(defaultVolume),
			Expected: 0,
			Error:    true, // Error expected because the difference is <= 0
		},
		{
			Name: "Only one volume created",
			Volume: func(volume Volume) Volume {
				volume.InitialSizeGb = 50
				volume.MaxLogicalSizeGb = 200
				volume.MaxCreatedVolumes = 1
				return volume
			}(defaultVolume),
			Expected: 0,
			Error:    true, // Error expected because we can't create any new volumes
		},
	}

	for _, i := range tests {

		got, err := i.Volume.calculateSizeIncreasePerVolume()

		if (err == nil) == i.Error {
			t.Errorf("calculateSizeIncreasePerVolume(%s) Returned an unexpected error: %s", i.Name, err)
		}
		if got != i.Expected {
			t.Errorf("calculateSizeIncreasePerVolume(%s) Expected: %d Got: %d", i.Name, i.Expected, got)
		}
	}
}
