package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	Root              string   `json:"root"`
	StateDir          string   `json:"state_dir"`
	DatabasePath      string   `json:"database_path"`
	LocalModel        string   `json:"local_model"`
	OllamaURL         string   `json:"ollama_url"`
	TargetContext     int      `json:"target_context_tokens"`
	SerenaCommand     string   `json:"serena_command"`
	SerenaArgs        []string `json:"serena_args"`
	SemanticProvider  string   `json:"semantic_provider"`
}

func Default(root string) Config {
	state := filepath.Join(root, ".project-brain")
	return Config{
		Root:             root,
		StateDir:         state,
		DatabasePath:     filepath.Join(state, "brain.db"),
		LocalModel:       "qwen3.5:4b",
		OllamaURL:        "http://127.0.0.1:11434",
		TargetContext:    30000,
		SemanticProvider: "serena",
		SerenaCommand:    "uvx",
		SerenaArgs:       []string{"--from", "git+https://github.com/oraios/serena", "serena", "start-mcp-server"},
	}
}

func Load(root string) (Config, error) {
	cfg := Default(root)
	path := filepath.Join(cfg.StateDir, "config.v2.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Root == "" {
		cfg.Root = root
	}
	if cfg.StateDir == "" {
		cfg.StateDir = filepath.Join(root, ".project-brain")
	}
	if cfg.DatabasePath == "" {
		cfg.DatabasePath = filepath.Join(cfg.StateDir, "brain.db")
	}
	return cfg, nil
}

func (c Config) Save() error {
	if err := os.MkdirAll(c.StateDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(c.StateDir, "config.v2.json"), data, 0o644)
}
