package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server     ServerConfig     `yaml:"server"`
	Redis      RedisConfig      `yaml:"redis"`
	MySQL      MySQLConfig      `yaml:"mysql"`
	Log        LogConfig        `yaml:"log"`
	RateLimit  RateLimitConfig  `yaml:"rate_limit"`
	Idempotent IdempotentConfig `yaml:"idempotent"`
	Shelf      ShelfConfig      `yaml:"shelf"`
}

type ServerConfig struct {
	Port int    `yaml:"port"`
	Mode string `yaml:"mode"`
}

type RedisConfig struct {
	Addr         string        `yaml:"addr"`
	Password     string        `yaml:"password"`
	DB           int           `yaml:"db"`
	PoolSize     int           `yaml:"pool_size"`
	MinIdleConns int           `yaml:"min_idle_conns"`
	MaxRetries   int           `yaml:"max_retries"`
	DialTimeout  time.Duration `yaml:"dial_timeout"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
}

type MySQLConfig struct {
	Host             string `yaml:"host"`
	Port             int    `yaml:"port"`
	Username         string `yaml:"username"`
	Password         string `yaml:"password"`
	Database         string `yaml:"database"`
	Charset          string `yaml:"charset"`
	MaxOpenConns     int    `yaml:"max_open_conns"`
	MaxIdleConns     int    `yaml:"max_idle_conns"`
	ConnMaxLifetime  int    `yaml:"conn_max_lifetime"`
	ConnMaxIdleTime  int    `yaml:"conn_max_idle_time"`
}

type LogConfig struct {
	Level      string `yaml:"level"`
	Filename   string `yaml:"filename"`
	MaxSize    int    `yaml:"max_size"`
	MaxBackups int    `yaml:"max_backups"`
	MaxAge     int    `yaml:"max_age"`
	Compress   bool   `yaml:"compress"`
}

type RateLimitConfig struct {
	PickupQPS  int `yaml:"pickup_qps"`
	LockQPS    int `yaml:"lock_qps"`
	BucketSize int `yaml:"bucket_size"`
}

type IdempotentConfig struct {
	TTLSeconds int64 `yaml:"ttl_seconds"`
}

type ShelfConfig struct {
	LockTimeoutSeconds int64 `yaml:"lock_timeout_seconds"`
}

var GlobalConfig *Config

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file failed: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config yaml failed: %w", err)
	}

	applyEnvOverrides(&cfg)
	GlobalConfig = &cfg
	return &cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	if port := os.Getenv("SERVER_PORT"); port != "" {
		fmt.Sscanf(port, "%d", &cfg.Server.Port)
	}
	if mode := os.Getenv("SERVER_MODE"); mode != "" {
		cfg.Server.Mode = mode
	}
	if redisAddr := os.Getenv("REDIS_ADDR"); redisAddr != "" {
		cfg.Redis.Addr = redisAddr
	}
	if redisPwd := os.Getenv("REDIS_PASSWORD"); redisPwd != "" {
		cfg.Redis.Password = redisPwd
	}
	if mysqlHost := os.Getenv("MYSQL_HOST"); mysqlHost != "" {
		cfg.MySQL.Host = mysqlHost
	}
	if mysqlPwd := os.Getenv("MYSQL_PASSWORD"); mysqlPwd != "" {
		cfg.MySQL.Password = mysqlPwd
	}
}

func (c *MySQLConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		c.Username, c.Password, c.Host, c.Port, c.Database, c.Charset)
}
