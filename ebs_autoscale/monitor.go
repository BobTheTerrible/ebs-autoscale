package ebs_autoscale

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

type MonitorVolume struct {
	Volume          Volume
	PollIntervalSec int32
	PercentageFull  float32
}

func NewMonitor(volume Volume, pollIntervalSec int32, percentageFull float32) *MonitorVolume {
	return &MonitorVolume{
		Volume:          volume,
		PollIntervalSec: pollIntervalSec,
		PercentageFull:  percentageFull,
	}
}

// Run assesses the file system usage. If the usage exceeds the configured amount, an attempt is made to grow the
// file system
func (m MonitorVolume) Run(ctx context.Context) error {

	slog.Info(fmt.Sprintf("Run: starting monitoring of: %s", m.Volume.Fs.GetMountPoint()))

	ticker := time.NewTicker(time.Duration(m.PollIntervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			err := m.assessAndGrow(ctx)
			if err != nil {
				return err
			}
			// TODO do I need to do this?? Best I can tell is that it restarts the ticker after work is done otherwise it simply keeps ticking in the background
			ticker.Reset(time.Duration(m.PollIntervalSec) * time.Second)
		case <-ctx.Done():
			slog.Info(fmt.Sprintf("Run: Aborting Monitoring of %s...\n", m.Volume.Fs.GetMountPoint()))
			return nil
		}
	}
}

// assessAndGrow checks the filesystem usage and grows the underlying volume if required
func (m MonitorVolume) assessAndGrow(ctx context.Context) error {

	usage, err := m.Volume.TotalUsagePercent()
	if err != nil {
		return err
	}

	if usage >= m.PercentageFull {
		slog.Info(fmt.Sprintf("assessAndGrow: usage threshold (%f) exceeded (%f), growing: %s", m.PercentageFull, usage, m.Volume.Fs.GetMountPoint()))

		err = m.Volume.GrowVolume(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}
