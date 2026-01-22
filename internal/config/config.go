package config

import (
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

type Config struct {
	OpenAI     OpenAIConfig
	Browser    BrowserConfig
	Agent      AgentConfig
	Logging    LoggingConfig
	RunDir     string
	ProfileDir string
}

type OpenAIConfig struct {
	APIKey     string
	Model      string
	Timeout    time.Duration
	MaxRetries int
	BaseURL    string // для совместимости с другими провайдерами
}

type BrowserConfig struct {
	ProfileDir        string
	Headless          bool
	Timeout           time.Duration
	NavigationTimeout time.Duration
	ActionTimeout     time.Duration // Таймаут для действий (click, type, etc.)
	ViewportWidth     int
	ViewportHeight    int
}

type AgentConfig struct {
	MaxSteps           int
	CriticInterval     int // вызывать Critic каждые N шагов
	EphemeralSize      int
	WorkingSummarySize int
	MaxRetries         int
	DryRun             bool
}

type LoggingConfig struct {
	Level      string
	Format     string // json | text
	OutputFile string
}

func Load() (*Config, error) {
	envPaths := []string{
		".env",
		"./.env",
		"../.env",
		"configs/.env",
	}

	var envLoaded bool
	var envPath string
	for _, path := range envPaths {
		if _, err := os.Stat(path); err == nil {
			if err := godotenv.Load(path); err != nil {
				return nil, fmt.Errorf("failed to load .env from %s: %w", path, err)
			}
			envLoaded = true
			envPath = path
			break
		}
	}

	if envLoaded {
		fmt.Printf("Loaded .env from: %s\n", envPath)
	} else {
		fmt.Println("Warning: .env file not found, using environment variables only")
	}

	viper.SetEnvPrefix("AGENT")
	viper.AutomaticEnv()

	cfg := &Config{
		OpenAI: OpenAIConfig{
			APIKey:     getEnv("OPENAI_API_KEY", ""),
			Model:      getEnv("OPENAI_MODEL", "gpt-4-turbo-preview"),
			Timeout:    parseDuration(getEnv("OPENAI_TIMEOUT", "5m")),
			MaxRetries: parseInt(getEnv("OPENAI_MAX_RETRIES", "3")),
			BaseURL:    getEnv("OPENAI_BASE_URL", ""),
		},
		Browser: BrowserConfig{
			ProfileDir:        getEnv("CHROME_PROFILE_DIR", ""),
			Headless:          false, // жёсткое требование
			Timeout:           parseDuration(getEnv("BROWSER_TIMEOUT", "30s")),
			NavigationTimeout: parseDuration(getEnv("BROWSER_NAVIGATION_TIMEOUT", "60s")),
			ActionTimeout:     parseDuration(getEnv("BROWSER_ACTION_TIMEOUT", "60s")),
			ViewportWidth:     parseInt(getEnv("BROWSER_VIEWPORT_WIDTH", "1920")),
			ViewportHeight:    parseInt(getEnv("BROWSER_VIEWPORT_HEIGHT", "1080")),
		},
		Agent: AgentConfig{
			MaxSteps:           parseInt(getEnv("AGENT_MAX_STEPS", "100")),
			CriticInterval:     parseInt(getEnv("AGENT_CRITIC_INTERVAL", "5")),
			EphemeralSize:      parseInt(getEnv("AGENT_EPHEMERAL_SIZE", "3")),
			WorkingSummarySize: parseInt(getEnv("AGENT_WORKING_SUMMARY_SIZE", "1500")),
			MaxRetries:         parseInt(getEnv("AGENT_MAX_RETRIES", "3")),
			DryRun:             getEnv("AGENT_DRY_RUN", "false") == "true",
		},
		Logging: LoggingConfig{
			Level:      getEnv("LOG_LEVEL", "info"),
			Format:     getEnv("LOG_FORMAT", "json"),
			OutputFile: getEnv("LOG_OUTPUT_FILE", ""),
		},
		RunDir: getEnv("RUN_DIR", "./runs"),
	}

	cfg.ProfileDir = cfg.Browser.ProfileDir

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	if c.OpenAI.APIKey == "" {
		return fmt.Errorf("OPENAI_API_KEY is required")
	}
	if c.Browser.ProfileDir == "" {
		return fmt.Errorf("CHROME_PROFILE_DIR is required")
	}
	if c.Browser.ProfileDir == "C:\\Users\\YourUsername\\AppData\\Local\\Google\\Chrome\\User Data" ||
		c.Browser.ProfileDir == "C:\\Users\\YourUsername\\AppData\\Local\\Google\\Chrome\\User Data\\Default" {
		return fmt.Errorf("CHROME_PROFILE_DIR contains placeholder 'YourUsername'. Please set the correct path to your Chrome profile")
	}
	if c.Browser.Headless {
		return fmt.Errorf("browser must be in headed mode (headless=false)")
	}
	return nil
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value != "" {
		return value
	}
	value = os.Getenv("AGENT_" + key)
	if value != "" {
		return value
	}
	return defaultValue
}

func parseInt(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}

func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 30 * time.Second // fallback
	}
	return d
}
