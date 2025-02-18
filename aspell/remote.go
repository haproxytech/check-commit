package aspell

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

func fetchRemoteFile(aspell Aspell) ([]string, error) {
	url := aspell.RemoteFile.URL
	if aspell.RemoteFile.URLEnv != "" {
		url = os.Getenv(aspell.RemoteFile.URLEnv)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if aspell.RemoteFile.HeaderFromENV != "" {
		envValue := os.Getenv(aspell.RemoteFile.HeaderFromENV)
		req.Header.Set(aspell.RemoteFile.HeaderFromENV, envValue)
	}
	if aspell.RemoteFile.PrivateTokenENV != "" {
		envValue := os.Getenv(aspell.RemoteFile.PrivateTokenENV)
		req.Header.Set("PRIVATE-TOKEN", envValue)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error fetching remote file: %s", resp.Status)
	}

	var data map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		return nil, err
	}

	var allowedWords []string

	items, ok := data[aspell.RemoteFile.AllowedItemsKey].([]interface{})
	if !ok {
		content, ok := data[aspell.RemoteFile.AllowedItemsKey].(string)
		if !ok {
			return nil, nil
		}
		if strings.HasPrefix(content, "```yaml\n") && strings.HasSuffix(content, "\n```") {
			content = strings.TrimPrefix(content, "```yaml\n")
			content = strings.TrimSuffix(content, "\n```")
			err = yaml.Unmarshal([]byte(content), &allowedWords)
			if err != nil {
				return nil, err
			}

			return allowedWords, nil
		}
		allowedWords = strings.Split(content, "\n")
	}

	for _, item := range items {
		allowedWords = append(allowedWords, item.(string))
	}

	return allowedWords, nil
}
