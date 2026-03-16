package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	Host       string        `yaml:"host"`
	Port       int           `yaml:"port"`
	Ollama     OllamaConfig  `yaml:"ollama"`
	MCPServers []MCPServer   `yaml:"mcp_servers"`
}

// OllamaConfig holds Ollama-specific settings
type OllamaConfig struct {
	BaseURL string `yaml:"base_url"`
	Model   string `yaml:"model"`
}

// MCPServer represents an MCP server configuration
type MCPServer struct {
	Name    string            `yaml:"name"`
	Type    string            `yaml:"type"` // "stdio" or "http"
	Command string            `yaml:"command,omitempty"`
	Args    []string          `yaml:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
	URL     string            `yaml:"url,omitempty"`
}

// Load reads configuration from a YAML file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Set defaults
	if cfg.Host == "" {
		cfg.Host = "0.0.0.0"
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if cfg.Ollama.BaseURL == "" {
		cfg.Ollama.BaseURL = "http://localhost:11434"
	}
	if cfg.Ollama.Model == "" {
		cfg.Ollama.Model = "llama3.2"
	}

	return &cfg, nil
}