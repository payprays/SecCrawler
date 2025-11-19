package bot

import (
	"SecCrawler/config"
	"SecCrawler/register"
	"SecCrawler/utils"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
)

type OneBotQQ struct{}

// OneBotMessage OneBot 消息结构
type OneBotMessage struct {
	Action string                 `json:"action"`
	Params map[string]interface{} `json:"params"`
}

// OneBotResponse OneBot 响应结构
type OneBotResponse struct {
	Status  string      `json:"status"`
	RetCode int         `json:"retcode"`
	Data    interface{} `json:"data"`
	Message string      `json:"message"`
}

func (bot OneBotQQ) Config() register.BotConfig {
	return register.BotConfig{
		Name: "OneBotQQ",
	}
}

// Send 推送消息到QQ
func (bot OneBotQQ) Send(crawlerResult [][]string, description string) error {
	apiURL := config.Cfg.Bot.OneBotQQ.API
	groupID := config.Cfg.Bot.OneBotQQ.GroupID
	userID := config.Cfg.Bot.OneBotQQ.UserID
	accessToken := config.Cfg.Bot.OneBotQQ.AccessToken
	timeout := config.Cfg.Bot.OneBotQQ.Timeout

	if apiURL == "" {
		return errors.New("OneBot API URL 未配置")
	}

	// 构建消息内容
	message := bot.buildMessage(crawlerResult, description)

	var err error
	// 优先发送到群组
	if groupID > 0 {
		err = bot.sendGroupMessage(apiURL, accessToken, groupID, message, timeout)
		if err != nil {
			fmt.Printf("[!] 发送到群组失败: %v\n", err)
		}
	}

	// 如果配置了私聊用户也发送
	if userID > 0 {
		err = bot.sendPrivateMessage(apiURL, accessToken, userID, message, timeout)
		if err != nil {
			fmt.Printf("[!] 发送私聊消息失败: %v\n", err)
		}
	}

	if groupID == 0 && userID == 0 {
		return errors.New("请至少配置 GroupID 或 UserID")
	}

	return err
}

// buildMessage 构建消息内容
func (bot OneBotQQ) buildMessage(crawlerResult [][]string, description string) string {
	var msgBuilder strings.Builder

	msgBuilder.WriteString(fmt.Sprintf("【%s 安全资讯】\n", description))
	msgBuilder.WriteString(fmt.Sprintf("时间: %s\n", utils.CurrentTime()))
	msgBuilder.WriteString(fmt.Sprintf("共 %d 条更新\n", len(crawlerResult)))
	msgBuilder.WriteString(strings.Repeat("=", 30) + "\n\n")

	for i, result := range crawlerResult {
		if len(result) >= 2 {
			title := result[1]
			url := result[0]

			msgBuilder.WriteString(fmt.Sprintf("%d. %s\n", i+1, title))
			msgBuilder.WriteString(fmt.Sprintf("🔗 %s\n\n", url))
		}

		// 限制消息长度，避免过长
		if msgBuilder.Len() > 4000 {
			msgBuilder.WriteString("... (内容过多，已截断)\n")
			break
		}
	}

	return msgBuilder.String()
}

// sendGroupMessage 发送群组消息
func (bot OneBotQQ) sendGroupMessage(apiURL, accessToken string, groupID int64, message string, timeout uint8) error {
	// 构建 OneBot 请求
	payload := OneBotMessage{
		Action: "send_group_msg",
		Params: map[string]interface{}{
			"group_id": groupID,
			"message":  message,
		},
	}

	return bot.sendRequest(apiURL, accessToken, payload, timeout)
}

// sendPrivateMessage 发送私聊消息
func (bot OneBotQQ) sendPrivateMessage(apiURL, accessToken string, userID int64, message string, timeout uint8) error {
	payload := OneBotMessage{
		Action: "send_private_msg",
		Params: map[string]interface{}{
			"user_id": userID,
			"message": message,
		},
	}

	return bot.sendRequest(apiURL, accessToken, payload, timeout)
}

// sendRequest 发送 HTTP 请求到 OneBot API
func (bot OneBotQQ) sendRequest(apiURL, accessToken string, payload OneBotMessage, timeout uint8) error {
	client := utils.BotClient(timeout)

	var reqURL string
	var jsonData []byte
	var err error

	// 判断 API 格式
	// 情况 1: 统一接口 (URL 以 / 结尾)，发送完整 Action + Params 结构
	if strings.HasSuffix(apiURL, "/") {
		reqURL = apiURL
		jsonData, err = json.Marshal(payload)
	} else {
		// 情况 2: 标准 HTTP 接口 (拼接 Action)，只发送 Params
		reqURL = apiURL + "/" + payload.Action
		jsonData, err = json.Marshal(payload.Params)
	}

	if err != nil {
		return fmt.Errorf("序列化请求失败: %v", err)
	}

	req, err := http.NewRequest("POST", reqURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// 添加 Access Token（如果配置了）
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("发送请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %v", err)
	}

	// 解析响应
	var oneBotResp OneBotResponse
	if err := json.Unmarshal(body, &oneBotResp); err != nil {
		// 如果不是 JSON 响应，检查 HTTP 状态码
		if resp.StatusCode != 200 {
			return fmt.Errorf("HTTP 错误: %d, 响应: %s", resp.StatusCode, string(body))
		}
		// 可能是简单的 OK 响应
		return nil
	}

	// 检查 OneBot 响应状态
	if oneBotResp.Status != "ok" && oneBotResp.RetCode != 0 {
		return fmt.Errorf("OneBot 错误: %s (retcode: %d)", oneBotResp.Message, oneBotResp.RetCode)
	}

	fmt.Println("[✓] OneBot QQ 消息发送成功")
	return nil
}
