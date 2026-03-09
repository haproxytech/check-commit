package aspell

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

func fetchRemoteFile(aspell Aspell) ([]string, error) {
	url := aspell.RemoteFile.URL
	if aspell.RemoteFile.URLEnv != "" {
		url = os.Getenv(aspell.RemoteFile.URLEnv)
		log.Printf("aspell remote file: using URL from env %s: %q", aspell.RemoteFile.URLEnv, url)
	} else {
		log.Printf("aspell remote file: using URL: %q", url)
	}

	if url == "" {
		return nil, errors.New("aspell remote file: URL is empty")
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("aspell remote file: failed to build request: %w", err)
	}

	if aspell.RemoteFile.HeaderFromENV != "" {
		envValue := os.Getenv(aspell.RemoteFile.HeaderFromENV)
		if envValue == "" {
			log.Printf("aspell remote file: warning: header env %s is empty", aspell.RemoteFile.HeaderFromENV)
		}
		req.Header.Set(aspell.RemoteFile.HeaderFromENV, envValue)
	}
	if aspell.RemoteFile.PrivateTokenENV != "" {
		envValue := os.Getenv(aspell.RemoteFile.PrivateTokenENV)
		if envValue == "" {
			log.Printf("aspell remote file: warning: private token env %s is empty", aspell.RemoteFile.PrivateTokenENV)
		}
		req.Header.Set("PRIVATE-TOKEN", envValue)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("aspell remote file: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error fetching remote file: %s", resp.Status)
	}

	var data map[string]any
	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		return nil, fmt.Errorf("aspell remote file: failed to decode JSON: %w", err)
	}

	var allowedWords []string

	items, ok := data[aspell.RemoteFile.AllowedItemsKey].([]any)
	if !ok {
		content, ok := data[aspell.RemoteFile.AllowedItemsKey].(string)
		if !ok {
			return nil, fmt.Errorf("aspell remote file: key %q not found or not a string/array in response", aspell.RemoteFile.AllowedItemsKey)
		}
		content = strings.TrimRight(content, "\n")
		if strings.HasPrefix(content, "```yaml\n") && strings.HasSuffix(content, "\n```") {
			content = strings.TrimPrefix(content, "```yaml\n")
			content = strings.TrimSuffix(content, "\n```")
			err = yaml.Unmarshal([]byte(content), &allowedWords)
			if err != nil {
				return nil, fmt.Errorf("aspell remote file: failed to parse YAML block: %w", err)
			}
			log.Printf("aspell remote file: loaded %d words (yaml block): %v", len(allowedWords), wordSample(allowedWords))
			return allowedWords, nil
		}
		allowedWords = strings.Split(content, "\n")
		log.Printf("aspell remote file: loaded %d words (newline-separated): %v", len(allowedWords), wordSample(allowedWords))
	} else {
		for _, item := range items {
			allowedWords = append(allowedWords, item.(string))
		}
		log.Printf("aspell remote file: loaded %d words (JSON array): %v", len(allowedWords), wordSample(allowedWords))
	}

	return allowedWords, nil
}

func wordSample(words []string) []string {
	if len(words) <= 3 {
		return words
	}
	return []string{words[0], words[1], "...", words[len(words)-1]}
}
