package operation_setting

import (
	"github.com/QuantumNous/new-api/setting/config"
)

type ModelDevSyncSetting struct {
	Enabled         bool    `json:"enabled"`
	SyncIntervalHours float64 `json:"sync_interval_hours"`
}

// Default configuration
var modelDevSyncSetting = ModelDevSyncSetting{
	Enabled:           true,
	SyncIntervalHours: 24,
}

func init() {
	// Register to global config manager
	config.GlobalConfig.Register("model_dev_sync_setting", &modelDevSyncSetting)
}

func GetModelDevSyncSetting() *ModelDevSyncSetting {
	return &modelDevSyncSetting
}

func GetModelDevSyncIntervalHours() float64 {
	if modelDevSyncSetting.SyncIntervalHours < 1 {
		return 24
	}
	return modelDevSyncSetting.SyncIntervalHours
}

func IsModelDevSyncEnabled() bool {
	return modelDevSyncSetting.Enabled
}
