package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type OpenAI struct {
	APIKey     string
	BaseURL    string
	ModelID    string
	HTTPClient *http.Client
}

func (o OpenAI) Model() string {
	if o.ModelID == "" {
		return "text-embedding-3-small"
	}
	return o.ModelID
}

func (o OpenAI) Embed(ctx context.Context, text string) ([]float32, error) {
	if strings.TrimSpace(o.APIKey) == "" {
		return nil, fmt.Errorf("openai api key is empty")
	}
	base := strings.TrimRight(o.BaseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	body, err := json.Marshal(map[string]any{
		"model": o.Model(),
		"input": text,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+o.APIKey)
	req.Header.Set("Content-Type", "application/json")
	client := o.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("embeddings http %d: %s", res.StatusCode, truncate(string(raw), 300))
	}
	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return nil, fmt.Errorf("embeddings: %s", parsed.Error.Message)
	}
	if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("embeddings: empty vector")
	}
	return parsed.Data[0].Embedding, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
