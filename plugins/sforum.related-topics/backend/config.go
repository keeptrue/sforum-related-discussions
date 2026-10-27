package main

import (
	"os"
	"strconv"
	"strings"
)

// Config 是插件运行期配置。
//
// 唯一来源是宿主启动插件子进程时注入的 SFORUM_SETTING_* 环境变量
// （host: Support/Extensions/plugin_command_transport.go 的 pluginSettingEnvName）。
// 从未保存过的设置不会被注入，所以这里的默认值必须与 manifest settings 的
// default 一致——否则「运营者没改过」和「运营者点了重置」会得到两种行为。
type Config struct {
	Enabled             bool
	Limit               int
	MinSimilarity       float64
	IncludeSameCategory bool
}

const (
	defaultLimit         = 4
	minLimit             = 1
	maxLimit             = 8
	defaultMinSimilarity = 0.15
	// candidateLimit 是候选集上限：先取 20 条相似度最高的，再在 Go 侧按
	// 同分类与时间衰减重排，避免 SQL 里做二次打分。
	candidateLimit = 20
	// candidateFloor 是进入候选集的最低字面相似度，用来挡住纯噪声。
	candidateFloor = 0.05
)

func loadConfig() Config {
	return Config{
		Enabled:             boolSetting(settingEnabled, true),
		Limit:               clampInt(intSetting(settingLimit, defaultLimit), minLimit, maxLimit),
		MinSimilarity:       clampFloat(floatSetting(settingMinSimilarity, defaultMinSimilarity), 0, 1),
		IncludeSameCategory: boolSetting(settingSameCategory, true),
	}
}

// settingEnv 复刻宿主 pluginSettingEnvName 的规则：非字母数字一律变下划线。
func settingEnv(key string) string {
	var value strings.Builder
	value.WriteString("SFORUM_SETTING_")
	for _, char := range strings.ToUpper(key) {
		if char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
			value.WriteRune(char)
		} else {
			value.WriteByte('_')
		}
	}
	return value.String()
}

func boolSetting(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(settingEnv(key)))
	if raw == "" {
		return fallback
	}
	switch strings.ToLower(raw) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return fallback
	}
}

func intSetting(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(settingEnv(key)))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

func floatSetting(key string, fallback float64) float64 {
	raw := strings.TrimSpace(os.Getenv(settingEnv(key)))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func clampFloat(value, low, high float64) float64 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
