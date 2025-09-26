///go:build unix

package ebs_autoscale

import (
	"gopkg.in/yaml.v3"
	"os"
)

type LoggingCfg struct {
	LogGroupName     string `yaml:"log-group-name" envconfig:"EBS_AUTO_LOGGING_LOG_GROUP_NAME"`
	PollIntervalSecs uint32 `yaml:"poll-interval" envconfig:"EBS_AUTO_LOGGING_POLL_INTERVAL_SEC" default:"5"`
	MaxBatchSize     uint32 `yaml:"max-batch-size" envconfig:"EBS_AUTO_LOGGING_MAX_BATCH_SIZE" default:"100"`
	Loglevel         string `yaml:"log-level" envconfig:"EBS_AUTO_LOGGING_LOG_LEVEL" default:"INFO"`
}

type MonitorCfg struct {
	Interval    int32   `yaml:"interval" envconfig:"EBS_AUTO_MONITOR_INTERVAL" default:"3"`
	ThresholdPc float32 `yaml:"threshold-pc" envconfig:"EBS_AUTO_MONITOR_THRESHOLD_PC" default:"50"`
}

type BackendCfg struct {
	Type       string                 `yaml:"type" envconfig:"EBS_AUTO_FILESYSTEM_TYPE"`
	FsSpecific map[string]interface{} `yaml:"fs-specific" envconfig:"EBS_AUTO_FILESYSTEM_FS_SPECIFIC"`
}

type VolumeCfg struct {
	MountPoint            string `yaml:"path" envconfig:"EBS_AUTO_FILESYSTEM_PATH" default:"/mnt/ebs-autoscale"`
	EbsType               string `yaml:"ebs-type" envconfig:"EBS_AUTO_FILESYSTEM_EBS_TYPE" default:"gp3"`
	EbsThroughput         *int32 `yaml:"ebs-throughput" envconfig:"EBS_AUTO_FILESYSTEM_EBS_THROUGHPUT"`
	EbsIops               *int32 `yaml:"ebs-ipos" envconfig:"EBS_AUTO_FILESYSTEM_EBS_IOPST"`
	InitialSizeGb         int32  `yaml:"initial-size-gb" envconfig:"EBS_AUTO_FILESYSTEM_INITIAL_SIZE" default:"100"`
	MaxSizeGb             int32  `yaml:"max-size-gb" envconfig:"EBS_AUTO_FILESYSTEM_MAX_SIZE" default:"500"`
	EbsMaxAttachedVolumes int32  `yaml:"ebs-max-attached-volumes" envconfig:"EBS_AUTO_FILESYSTEM_MAX_ATTACHED_VOLUMES" default:"16"`
	EbsMaxCreatedVolumes  int32  `yaml:"ebs-max-created-volumes" envconfig:"EBS_AUTO_FILESYSTEM_MAX_CREATED_VOLUMES" default:"5"`
	Backend               *BackendCfg  `yaml:"backend"`
}

type Config struct {
	Logging *LoggingCfg `yaml:"logging"`
	Monitor MonitorCfg  `yaml:"monitor"`
	Volume  VolumeCfg   `yaml:"filesystem"`
}

// NewConfig marshals the given path into a Config object. It will then look at environment variables for values to
// override.
func NewConfig(path string) (*Config, error) {
	var cfg Config
	err := readFile(&cfg, path)
	if err != nil {
		return nil, err
	}

	// Initialize Backend to an empty struct if not provided
	if cfg.Volume.Backend == nil {
		cfg.Volume.Backend = &BackendCfg{}
	}

	// Initialize FsSpecific to an empty map if not provided
	if cfg.Volume.Backend.FsSpecific == nil {
		cfg.Volume.Backend.FsSpecific = make(map[string]interface{})
	}

	// If the Backend Type is empty, set it to "btrfs"
	if cfg.Volume.Backend.Type == "" {
		cfg.Volume.Backend.Type = "btrfs"
	}

	// TODO this is not working as expected...
	//err = readEnv(&cfg)
	//if err != nil {
	//	return nil, err
	//}
	return &cfg, nil
}

func readFile(cfg *Config, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck

	decoder := yaml.NewDecoder(f)
	return decoder.Decode(cfg)
}

// TODO see above...
//func readEnv(cfg *Config) error {
//	return envconfig.Process("", cfg)
//}
