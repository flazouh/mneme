package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DataDir        string
	Socket         string
	Lock           string
	DBPath         string
	ArchiveRepo    string
	ArchivePush    bool
	OpenAIKey      string
	OpenAIBaseURL  string
	EmbeddingModel string
	UserID         string
	HashEmbed      bool
}

func Load() Config {
	loadDotEnv(filepath.Join(home(), ".config", "mneme", "config.env"))
	data := first(
		os.Getenv("MNEME_DATA_DIR"),
		filepath.Join(home(), ".local", "share", "mneme"),
	)
	sock := first(os.Getenv("MNEME_SOCKET"), filepath.Join(data, "mneme.sock"))
	return Config{
		DataDir:        data,
		Socket:         sock,
		Lock:           first(os.Getenv("MNEME_LOCK"), filepath.Join(data, "mneme.lock")),
		DBPath:         first(os.Getenv("MNEME_DB"), filepath.Join(data, "mneme.db")),
		ArchiveRepo:    os.Getenv("MNEME_ARCHIVE_REPO"),
		ArchivePush:    boolEnv("MNEME_ARCHIVE_PUSH", false),
		OpenAIKey:      first(os.Getenv("MNEME_OPENAI_API_KEY"), os.Getenv("OPENAI_API_KEY")),
		OpenAIBaseURL:  first(os.Getenv("MNEME_OPENAI_BASE_URL"), os.Getenv("OPENAI_BASE_URL"), "https://openrouter.ai/api/v1"),
		EmbeddingModel: first(os.Getenv("MNEME_EMBEDDING_MODEL"), os.Getenv("MEMORY_EMBEDDING_MODEL"), "openai/text-embedding-3-small"),
		UserID:         first(os.Getenv("MNEME_USER_ID"), "ace"),
		HashEmbed:      boolEnv("MNEME_HASH_EMBED", false),
	}
}

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

func first(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func boolEnv(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return v
}

func loadDotEnv(path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		if os.Getenv(key) == "" {
			_ = os.Setenv(key, value)
		}
	}
}

func DurationEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return d
}
