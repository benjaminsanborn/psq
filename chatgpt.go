package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) callChatGPT(prompt string) tea.Cmd {
	return func() tea.Msg {
		// Get OpenAI API key from environment
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return chatgptErrorMsg("OPENAI_API_KEY environment variable not set")
		}

		// Prepare the request - include current query content if it exists
		var fullPrompt string
		currentSQL := strings.TrimSpace(m.sqlTextarea.Value())
		if currentSQL != "" {
			fullPrompt = fmt.Sprintf("Modify the following PostgreSQL query based on this request: %s\n\nCurrent query:\n%s\n\nPlease respond with ONLY the modified SQL query, no explanations or additional text. Use the documentation available at https://www.postgresql.org/docs/current/index.html to provide a validated query that will work against the current major version. This query will be used to power a UI used to provide insight into the prompted question, so consider adding as much related data (like joining pids to their process query), joining relnames to their table names, using pg_size_pretty when rendering data volume and using left() to truncate any column data that might get too long to display in a normal shell window.", prompt, currentSQL)
		} else {
			fullPrompt = fmt.Sprintf("Generate a PostgreSQL query for the following request: %s\n\nPlease respond with ONLY the SQL query, no explanations or additional text. Use the documentation available at https://www.postgresql.org/docs/current/index.html to provide a validated query that will work against the current major version. This query will be used to power a UI used to provide insight into the prompted question, so consider adding as much related data (like joining pids to their process query), joining relnames to their table names, using pg_size_pretty when rendering data volume and using left() to truncate any column data that might get too long to display in a normal shell window.", prompt)
		}

		reqBody := ChatGPTRequest{
			Model: "gpt-4o-mini",
			Messages: []Message{
				{
					Role:    "user",
					Content: fullPrompt,
				},
			},
		}

		jsonData, err := json.Marshal(reqBody)
		if err != nil {
			return chatgptErrorMsg(fmt.Sprintf("Failed to marshal request: %v", err))
		}

		// Make the API call
		req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(jsonData))
		if err != nil {
			return chatgptErrorMsg(fmt.Sprintf("Failed to create request: %v", err))
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return chatgptErrorMsg(fmt.Sprintf("Failed to make API call: %v", err))
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return chatgptErrorMsg(fmt.Sprintf("API error (status %d): %s", resp.StatusCode, string(body)))
		}

		var chatResp ChatGPTResponse
		if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
			return chatgptErrorMsg(fmt.Sprintf("Failed to decode response: %v", err))
		}

		if len(chatResp.Choices) == 0 {
			return chatgptErrorMsg("No response from ChatGPT")
		}

		sql := strings.TrimSpace(chatResp.Choices[0].Message.Content)

		// Clean up the response - remove code block markers if present
		sql = strings.TrimPrefix(sql, "```sql")
		sql = strings.TrimPrefix(sql, "```")
		sql = strings.TrimSuffix(sql, "```")
		sql = strings.TrimSpace(sql)

		return chatgptResponseMsg(sql)
	}
}

func (m *Model) handleChatGPTResponse(sql string) tea.Cmd {
	// Always directly populate SQL textarea
	if m.editMode {
		m.sqlTextarea.SetValue(sql)
		m.chatgptResponse = ""

		// If we were in AI prompt mode, clear that too
		if m.editFocus == 4 {
			m.aiPromptInput.SetValue("")
		}

		// Focus on SQL textarea
		m.editFocus = 3
		m.sqlTextarea.Focus()
		m.nameInput.Blur()
		m.descInput.Blur()
		m.orderInput.Blur()
		m.aiPromptInput.Blur()
	}

	return nil
}

func (m *Model) handleChatGPTError(errMsg string) {
	m.err = errMsg
}
