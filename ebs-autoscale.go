package main

import (
	"context"
	"flag"
	"fmt"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/BobTheTerrible/ebs-autoscale/ebs_autoscale"
	"github.com/BobTheTerrible/ebs-autoscale/ebs_autoscale/filesystem"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
)

var VersionName string

var logLevelMap map[string]slog.Level

const (
	exitCodeInterrupt = 2
	defaultConfigPath = "/etc/ebs-autoscale/ebs-autoscale.json"
)

func init() {
	logLevelMap = map[string]slog.Level{
		"INFO":  slog.LevelInfo,
		"DEBUG": slog.LevelDebug,
		"ERROR": slog.LevelError,
		"WARN":  slog.LevelWarn,
	}
}

func main() {

	// Set up a context to capture interrupt, so we can gracefully shut down loops. This helps protect against orphan
	// ebs volumes.
	ctx, cancel := context.WithCancel(context.Background())
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)
	defer func() {
		signal.Stop(signalChan)
		cancel()
	}()
	go func() {
		select {
		case <-signalChan: // first signal, cancel context
			cancel()
		case <-ctx.Done():
		}
		<-signalChan // second signal, hard exit
		os.Exit(exitCodeInterrupt)
	}()

	switch os.Args[1] {

	case "init":
		createVolume(ctx, os.Args[2:])
	case "grow":
		growVolume(ctx, os.Args[2:])
	case "monitor":
		monitorVolume(ctx, os.Args[2:])
	case "version":
		fmt.Printf("Version: %s", VersionName)
	}
}

func createVolume(ctx context.Context, args []string) *ebs_autoscale.Volume {

	cmd := flag.NewFlagSet("init", flag.ExitOnError)
	configPath := cmd.String("config", defaultConfigPath, "Path to a json config file")

	err := cmd.Parse(args)
	if err != nil {
		log.Fatalln(err)
	}

	config, volume, err := base(ctx, *configPath)
	if err != nil {
		log.Fatalln(err)
	}

	slog.Info(fmt.Sprintf("createVolume: Creating New volume: %s", config.Volume.MountPoint))

	err = volume.CreateVolume(ctx)
	if err != nil {
		log.Fatalln(err)
	}

	return volume
}

func growVolume(ctx context.Context, args []string) *ebs_autoscale.Volume {

	cmd := flag.NewFlagSet("grow", flag.ExitOnError)
	configPath := cmd.String("config", defaultConfigPath, "Path to a json config file")

	err := cmd.Parse(args)
	if err != nil {
		log.Fatalln(err)
	}

	_, volume, err := base(ctx, *configPath)
	if err != nil {
		log.Fatalln(err)
	}

	slog.Info("growVolume: Growing volume")

	err = volume.GrowVolume(ctx)
	if err != nil {
		log.Fatalln(err)
	}

	return volume
}

func monitorVolume(ctx context.Context, args []string) *ebs_autoscale.MonitorVolume {

	cmd := flag.NewFlagSet("monitor", flag.ExitOnError)
	configPath := cmd.String("config", defaultConfigPath, "Path to a json config file")

	err := cmd.Parse(args)
	if err != nil {
		log.Fatalln(err)
	}

	config, volume, err := base(ctx, *configPath)
	if err != nil {
		log.Fatalln(err)
	}

	monitor := ebs_autoscale.NewMonitor(
		*volume,
		config.Monitor.Interval,
		config.Monitor.ThresholdPc,
	)

	slog.Info(fmt.Sprintf("monitorVolume: Monitoring volume: %s", config.Volume.MountPoint))

	err = monitor.Run(ctx)
	if err != nil {
		log.Fatalln(err)
	}

	return monitor
}

func base(ctx context.Context, configPath string) (*ebs_autoscale.Config, *ebs_autoscale.Volume, error) {

	config, err := ebs_autoscale.NewConfig(configPath)
	if err != nil {
		return nil, nil, err
	}

	host, err := ebs_autoscale.NewEc2Host(ctx)
	if err != nil {
		return nil, nil, err
	}

	fs, err := filesystem.GetFileSystem(config.Volume.Backend.Type, config.Volume.MountPoint, config.Volume.Backend.FsSpecific)
	if err != nil {
		return nil, nil, err
	}

	volume, err := ebs_autoscale.NewVolume(
		ctx,
		*host,
		fs,
		config.Volume,
	)
	if err != nil {
		return config, nil, err
	}

	// If the config has defined logging set up the cloudwatch logger
	if config.Logging != nil {
		_, err := initLogger(ctx, volume.Host.Region, *config.Logging, fmt.Sprintf("ebs-autoscale/%s: ", volume.Host.InstanceId))
		if err != nil {
			log.Fatalln(err)
		}
	}

	return config, volume, nil
}

func initLogger(ctx context.Context, region string, cfg ebs_autoscale.LoggingCfg, prefix string) (*ebs_autoscale.CwLogWriter, error) {

	slog.Info(fmt.Sprintf("initLogger: Init cloudwatch logger to: %s", cfg.LogGroupName))

	logLevel, ok := logLevelMap[cfg.Loglevel]
	if !ok {
		return nil, fmt.Errorf("initLogger: unregognised log level string: %s", cfg.Loglevel)
	}

	writer := ebs_autoscale.NewCwLogWriter(
		cfg.LogGroupName,
		cfg.PollIntervalSecs,
		cfg.MaxBatchSize,
	)

	awsConf, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithDefaultRegion(region))
	if err != nil {
		return nil, err
	}

	client := cloudwatchlogs.NewFromConfig(awsConf)
	writer.Start(ctx, *client)

	logWriter := log.Writer()

	log.SetOutput(io.MultiWriter(logWriter, writer))
	slog.SetLogLoggerLevel(logLevel)
	log.SetPrefix(prefix)

	// Print out errors from the logger as they happen
	// Abort gracefully...
	go func() {
		for logError := true; logError; {
			select {
			case err, ok := <-writer.ErrChannel:
				if !ok {
					logError = false
				}
				slog.Error(fmt.Sprintf("initLogger: %s", err.Error()))
			case <-ctx.Done():
				logError = false
			}
		}
		log.SetOutput(logWriter)
	}()

	return writer, nil
}
