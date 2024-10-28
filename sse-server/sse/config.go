package sse

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
)

type Config struct {
	ServerAddr   string `yaml:"server_addr"`
	ServerPort   string `yaml:"server_port"`
	AdminToken   string `yaml:"admin_token"`
	LogLevel     string `yaml:"log_level"`
	LogAddSource bool   `yaml:"log_add_source"`
	// Connection control can be specified here
	Redis struct {
		Addr     string `yaml:"addr"`
		Password string `yaml:"password"`
		DB       int    `yaml:"db"`
	}
}

func LoadConfig(path string) (*Config, error) {
	conf, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file %s: %w", path, err)
	}
	defer conf.Close()

	config := &Config{}

	decoder := yaml.NewDecoder(conf)
	err = decoder.Decode(config)
	if err != nil {
		return nil, err
	}

	if config.ServerAddr == "" || config.ServerPort == "" {
		return nil, fmt.Errorf("config file missing required fields: server_addr or server_port")
	}

	return config, nil
}
