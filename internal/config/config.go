package config

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

// Config is the root configuration struct.
// personal_data on disk is stored encrypted as personal_data_encrypted (base64).
type Config struct {
	Service      ServiceConfig  `yaml:"service"`
	Telegram     TelegramConfig `yaml:"telegram"`
	PersonalData PersonalData   `yaml:"-"` // populated after decryption; never written to disk
	WatchList    []WatchEntry   `yaml:"watch_list"`
}

type ServiceConfig struct {
	BaseURL             string `yaml:"base_url"`
	UseSSE              bool   `yaml:"use_sse"`
	PollIntervalSeconds int    `yaml:"poll_interval_seconds"`
}

type TelegramConfig struct {
	Enabled  bool   `yaml:"enabled"`
	BotToken string `yaml:"bot_token"`
	ChatID   string `yaml:"chat_id"`
}

type PersonalData struct {
	// Separate name fields (preferred — used directly in GHB registration form).
	// If LastName/FirstName are empty, FullName is split by spaces:
	//   "Иванов Иван Иванович" → LastName="Иванов" FirstName="Иван" MiddleName="Иванович"
	LastName   string `yaml:"last_name,omitempty"`
	FirstName  string `yaml:"first_name,omitempty"`
	MiddleName string `yaml:"middle_name,omitempty"`

	// Convenience alias: "LastName FirstName MiddleName" space-separated.
	FullName string `yaml:"full_name,omitempty"`

	// Phone in any format — the registrar strips non-digits and takes last 9.
	// E.g. "+375 29 284 40 73" → "292844073"
	Phone string `yaml:"phone"`
}

// Parts returns (lastName, firstName, middleName), splitting FullName if needed.
func (p PersonalData) Parts() (last, first, middle string) {
	last, first, middle = p.LastName, p.FirstName, p.MiddleName
	if last == "" && p.FullName != "" {
		parts := strings.Fields(p.FullName)
		if len(parts) >= 1 {
			last = parts[0]
		}
		if len(parts) >= 2 {
			first = parts[1]
		}
		if len(parts) >= 3 {
			middle = strings.Join(parts[2:], " ")
		}
	}
	return
}

// PhoneDigits returns last 9 digits of the phone number (GHB form format).
func (p PersonalData) PhoneDigits() string {
	var digits []rune
	for _, ch := range p.Phone {
		if ch >= '0' && ch <= '9' {
			digits = append(digits, ch)
		}
	}
	if len(digits) >= 9 {
		return string(digits[len(digits)-9:])
	}
	return string(digits)
}

type WatchEntry struct {
	ObjectExternalID string `yaml:"object_external_id"`
	NotifyOnOpen     bool   `yaml:"notify_on_open"`
	AutoRegister     bool   `yaml:"auto_register"`
}

// rawConfig is used for YAML parsing (includes encrypted personal_data field).
type rawConfig struct {
	Service               ServiceConfig  `yaml:"service"`
	Telegram              TelegramConfig `yaml:"telegram"`
	PersonalDataEncrypted string         `yaml:"personal_data_encrypted,omitempty"`
	WatchList             []WatchEntry   `yaml:"watch_list"`
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

// InitConfig creates a new encrypted config file via interactive prompts.
func InitConfig(path string) error {
	// Check if file already exists
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("Файл %s уже существует. Перезаписать? [y/N] ", path)
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(answer)) != "y" {
			fmt.Println("Отменено.")
			return nil
		}
	}

	fmt.Println("=== Инициализация конфигурации worker-ghb-http ===")
	fmt.Println()

	// Collect service config
	svc := ServiceConfig{
		BaseURL:             prompt("URL сервиса мониторинга [https://stroi.homes]: ", "https://stroi.homes"),
		UseSSE:              promptBool("Использовать SSE-стрим? [Y/n]: ", true),
		PollIntervalSeconds: 60,
	}

	// Collect telegram config
	tgEnabled := promptBool("Включить интеграцию с Telegram? [Y/n]: ", true)
	var tg TelegramConfig
	if tgEnabled {
		tg = TelegramConfig{
			Enabled:  true,
			BotToken: prompt("Telegram bot token: ", ""),
			ChatID:   prompt("Telegram chat_id: ", ""),
		}
	}

	// Collect watch list
	fmt.Println()
	fmt.Println("Watch list — объекты для мониторинга.")
	fmt.Println("Введите external_id (или * для всех объектов). Пустая строка — завершить.")
	var watchList []WatchEntry
	reader := bufio.NewReader(os.Stdin)
	for {
		eid := promptReader(reader, "  object_external_id (Enter = стоп): ", "")
		if eid == "" {
			break
		}
		notifyOnOpen := promptBool("  Уведомлять при открытии? [Y/n]: ", true)
		autoReg := promptBool("  Авторегистрация? [y/N]: ", false)
		watchList = append(watchList, WatchEntry{
			ObjectExternalID: eid,
			NotifyOnOpen:     notifyOnOpen,
			AutoRegister:     autoReg,
		})
	}

	// Collect personal data if any entry needs auto_register
	var encryptedPD string
	needsPersonalData := false
	for _, e := range watchList {
		if e.AutoRegister {
			needsPersonalData = true
			break
		}
	}

	if needsPersonalData {
		fmt.Println()
		fmt.Println("Заполните персональные данные для авторегистрации.")
		fmt.Println("Они будут зашифрованы AES-256-GCM.")
		pd := PersonalData{
			FullName: prompt("  ФИО (полностью): ", ""),
			Phone:    prompt("  Телефон (+375...): ", ""),
		}
		pdYAML, err := yaml.Marshal(pd)
		if err != nil {
			return fmt.Errorf("marshal personal_data: %w", err)
		}

		password, err := promptPassword("Придумайте пароль для шифрования: ")
		if err != nil {
			return fmt.Errorf("read password: %w", err)
		}
		confirm, err := promptPassword("Повторите пароль: ")
		if err != nil {
			return fmt.Errorf("read password: %w", err)
		}
		if password != confirm {
			return fmt.Errorf("пароли не совпадают")
		}

		ciphertext, err := Encrypt(pdYAML, password)
		if err != nil {
			return fmt.Errorf("encrypt personal_data: %w", err)
		}
		encryptedPD = base64.StdEncoding.EncodeToString(ciphertext)
		fmt.Println("Персональные данные зашифрованы.")
	}

	// Write config file
	raw := rawConfig{
		Service:               svc,
		Telegram:              tg,
		PersonalDataEncrypted: encryptedPD,
		WatchList:             watchList,
	}
	data, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Printf("\nКонфиг сохранён: %s\n", path)
	fmt.Println("Запустите: ./worker-ghb-http --config", path)
	return nil
}

// EditConfig decrypts personal_data, opens $EDITOR, re-encrypts and saves.
func EditConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	// Decrypt personal_data if present
	var plainPD []byte
	var password string
	if raw.PersonalDataEncrypted != "" {
		pw, err := promptPassword("Введите пароль для расшифровки: ")
		if err != nil {
			return fmt.Errorf("read password: %w", err)
		}
		password = pw

		ciphertext, err := base64.StdEncoding.DecodeString(raw.PersonalDataEncrypted)
		if err != nil {
			return fmt.Errorf("decode encrypted data: %w", err)
		}
		plainPD, err = Decrypt(ciphertext, password)
		if err != nil {
			return fmt.Errorf("decrypt personal_data: %w", err)
		}
	}

	// Build editable config (with plain personal_data)
	type editableConfig struct {
		Service      ServiceConfig  `yaml:"service"`
		Telegram     TelegramConfig `yaml:"telegram"`
		PersonalData PersonalData   `yaml:"personal_data,omitempty"`
		WatchList    []WatchEntry   `yaml:"watch_list"`
	}
	var pd PersonalData
	if len(plainPD) > 0 {
		if err := yaml.Unmarshal(plainPD, &pd); err != nil {
			return fmt.Errorf("parse personal_data: %w", err)
		}
	}
	editable := editableConfig{
		Service:      raw.Service,
		Telegram:     raw.Telegram,
		PersonalData: pd,
		WatchList:    raw.WatchList,
	}
	editableYAML, err := yaml.Marshal(editable)
	if err != nil {
		return fmt.Errorf("marshal editable config: %w", err)
	}

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "worker-ghb-http-*.yaml")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(editableYAML); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	tmpFile.Close()

	// Open editor
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor: %w", err)
	}

	// Read edited file
	editedData, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("read temp file: %w", err)
	}
	var edited editableConfig
	if err := yaml.Unmarshal(editedData, &edited); err != nil {
		return fmt.Errorf("parse edited config: %w", err)
	}

	// Re-encrypt personal_data
	var newEncryptedPD string
	// Check if personal_data has any non-empty fields
	if edited.PersonalData.FullName != "" || edited.PersonalData.Phone != "" {
		if password == "" {
			pw, err := promptPassword("Введите пароль для шифрования personal_data: ")
			if err != nil {
				return fmt.Errorf("read password: %w", err)
			}
			password = pw
		}
		pdYAML, err := yaml.Marshal(edited.PersonalData)
		if err != nil {
			return fmt.Errorf("marshal personal_data: %w", err)
		}
		ciphertext, err := Encrypt(pdYAML, password)
		if err != nil {
			return fmt.Errorf("encrypt personal_data: %w", err)
		}
		newEncryptedPD = base64.StdEncoding.EncodeToString(ciphertext)
	}

	// Write updated config
	newRaw := rawConfig{
		Service:               edited.Service,
		Telegram:              edited.Telegram,
		PersonalDataEncrypted: newEncryptedPD,
		WatchList:             edited.WatchList,
	}
	newData, err := yaml.Marshal(newRaw)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, newData, 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Printf("Конфиг обновлён: %s\n", path)
	return nil
}

// getPassword returns the encryption password from WORKER_PASSWORD env or interactive prompt.
func getPassword() (string, error) {
	if pw := os.Getenv("WORKER_PASSWORD"); pw != "" {
		return pw, nil
	}
	return promptPassword("Введите пароль для расшифровки personal_data: ")
}

// promptPassword reads a password without echoing it to the terminal.
func promptPassword(label string) (string, error) {
	fmt.Print(label)
	// Try secure terminal input
	if term.IsTerminal(int(os.Stdin.Fd())) {
		pw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		return string(pw), nil
	}
	// Fallback for non-interactive (e.g. pipe)
	reader := bufio.NewReader(os.Stdin)
	pw, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return strings.TrimSpace(pw), nil
}

// prompt reads a line with a default value.
func prompt(label, defaultVal string) string {
	reader := bufio.NewReader(os.Stdin)
	return promptReader(reader, label, defaultVal)
}

func promptReader(reader *bufio.Reader, label, defaultVal string) string {
	fmt.Print(label)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal
	}
	return line
}

// promptBool reads a yes/no answer with a default.
func promptBool(label string, defaultVal bool) bool {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print(label)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	switch line {
	case "y", "yes", "да":
		return true
	case "n", "no", "нет":
		return false
	default:
		return defaultVal
	}
}
