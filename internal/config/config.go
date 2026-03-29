package config

import (
	"encoding/base64"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration struct.
// personal_data on disk is stored encrypted as personal_data_encrypted (base64).
type Config struct {
	Service      ServiceConfig      `yaml:"service"`
	Telegram     TelegramConfig     `yaml:"telegram"`
	PersonalData PersonalData       `yaml:"-"` // populated after decryption; never written to disk
	WatchList    []WatchEntry       `yaml:"watch_list"`
}

type ServiceConfig struct {
	BaseURL             string `yaml:"base_url"`
	UseSSE              bool   `yaml:"use_sse"`
	PollIntervalSeconds int    `yaml:"poll_interval_seconds"`
}

type TelegramConfig struct {
	BotToken string `yaml:"bot_token"`
	ChatID   string `yaml:"chat_id"`
}

type PersonalData struct {
	FullName  string `yaml:"full_name"`
	Phone     string `yaml:"phone"`
	BirthDate string `yaml:"birth_date"`
}

type WatchEntry struct {
	ObjectExternalID string `yaml:"object_external_id"`
	NotifyOnOpen     bool   `yaml:"notify_on_open"`
	AutoRegister     bool   `yaml:"auto_register"`
}

// rawConfig is used for YAML parsing (includes encrypted personal_data field).
type rawConfig struct {
	Service                  ServiceConfig `yaml:"service"`
	Telegram                 TelegramConfig `yaml:"telegram"`
	PersonalDataEncrypted    string         `yaml:"personal_data_encrypted,omitempty"`
	WatchList                []WatchEntry   `yaml:"watch_list"`
}

// Load reads and decrypts the config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg := &Config{
		Service:   raw.Service,
		Telegram:  raw.Telegram,
		WatchList: raw.WatchList,
	}

	// Decrypt personal_data if present
	if raw.PersonalDataEncrypted != "" {
		password, err := getPassword()
		if err != nil {
			return nil, fmt.Errorf("get password: %w", err)
		}
		ciphertext, err := base64.StdEncoding.DecodeString(raw.PersonalDataEncrypted)
		if err != nil {
			return nil, fmt.Errorf("decode personal_data_encrypted: %w", err)
		}
		plaintext, err := Decrypt(ciphertext, password)
		if err != nil {
			return nil, fmt.Errorf("decrypt personal_data: %w", err)
		}
		var pd PersonalData
		if err := yaml.Unmarshal(plaintext, &pd); err != nil {
			return nil, fmt.Errorf("parse personal_data: %w", err)
		}
		cfg.PersonalData = pd
	}

	// Set defaults
	if cfg.Service.PollIntervalSeconds <= 0 {
		cfg.Service.PollIntervalSeconds = 60
	}

	return cfg, nil
}

// InitConfig creates a new config file with interactive prompts.
func InitConfig(path string) error {
	fmt.Println("Создание нового конфига:", path)
	fmt.Println("TODO: интерактивный wizard для создания зашифрованного конфига")
	return nil
}

// EditConfig decrypts personal_data, opens editor, re-encrypts and saves.
func EditConfig(path string) error {
	fmt.Println("Редактирование конфига:", path)
	fmt.Println("TODO: открыть временный файл в $EDITOR, перешифровать после сохранения")
	return nil
}

// getPassword returns the encryption password from WORKER_PASSWORD env or interactive prompt.
func getPassword() (string, error) {
	if pw := os.Getenv("WORKER_PASSWORD"); pw != "" {
		return pw, nil
	}
	fmt.Print("Введите пароль для расшифровки personal_data: ")
	var pw string
	if _, err := fmt.Scanln(&pw); err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return pw, nil
}
