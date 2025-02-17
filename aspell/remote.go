package aspell

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

func fetchRemoteFile(aspell Aspell) ([]string, error) {
	url := aspell.RemoteFile.URL
	if url == "" {
		return []string{}, nil
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if aspell.RemoteFile.HeaderFromENV != "" {
		envValue := os.Getenv(aspell.RemoteFile.HeaderFromENV)
		req.Header.Set(aspell.RemoteFile.HeaderFromENV, envValue)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

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
