package socialmedia

import (
	"SecCrawler/config"
	"SecCrawler/register"
	"SecCrawler/utils"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/g8rswimmer/go-twitter/v2"
	twitterscraper "github.com/n0madic/twitter-scraper"
)

type authorizer struct {
	Token string
}

func (a *authorizer) Add(req *http.Request) {
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", a.Token))
}

type X struct{}

// getTargetUsers 从配置文件获取目标用户列表
func getTargetUsers() []string {
	// 尝试从 x-kit/dev-accounts.json 读取
	fileContent, err := os.ReadFile("x-kit/dev-accounts.json")
	if err == nil {
		type Account struct {
			Username string `json:"username"`
		}
		var accounts []Account
		if err := json.Unmarshal(fileContent, &accounts); err == nil {
			var users []string
			for _, acc := range accounts {
				users = append(users, acc.Username)
			}
			if len(users) > 0 {
				return users
			}
		}
	}

	return config.Cfg.Crawler.SocialMedia.X.IDs
}

func (x X) Config() register.CrawlerConfig {
	return register.CrawlerConfig{
		Name:        "SocialMedia.X",
		Description: "X(Twitter)平台安全情报聚合",
	}
}

// Get 获取X平台前24小时内推文
func (x X) Get() ([][]string, error) {
	// 优先尝试使用 x-kit (基于 Cookie 的爬虫)
	fmt.Println("[*] 尝试使用 x-kit (Cookie 爬虫)...")
	tweets, err := x.fetchWithXKit()
	if err == nil {
		return tweets, nil
	}
	fmt.Printf("[!] x-kit 调用失败: %v，尝试其他方案...\n", err)

	// API v2 needs a bearer token. In the free plan, this is often the same as the access token.
	bearerToken := config.Cfg.Crawler.SocialMedia.X.AccessToken

	if bearerToken != "" {
		fmt.Println("[*] 尝试使用 Twitter API V2...")
		tweets, err := x.fetchWithAPIV2(bearerToken)
		if err != nil {
			fmt.Printf("[!] API V2 调用失败: %v，切换到免费方案...\n", err)
			return x.fetchWithScraper()
		}
		return tweets, nil
	}

	fmt.Println("[*] 未配置 API V2 的 Bearer/Access Token，使用免费爬虫方案...")
	return x.fetchWithScraper()
}

// fetchWithXKit 使用 x-kit 脚本获取推文
func (x X) fetchWithXKit() ([][]string, error) {
	var resultSlice [][]string
	targetUsers := getTargetUsers()
	fmt.Printf("[*] 使用 x-kit 监控 %d 个用户账号\n", len(targetUsers))

	// x-kit 目录路径
	xKitPath := "x-kit"

	for _, username := range targetUsers {
		fmt.Printf("[*] 正在使用 x-kit 爬取 @%s...\n", username)

		// 执行 bun run scripts/crawl-user.ts <username>
		cmd := exec.Command("bun", "run", "scripts/crawl-user.ts", username)
		cmd.Dir = xKitPath
		output, err := cmd.CombinedOutput()
		if err != nil {
			outputStr := string(output)
			fmt.Printf("[!] x-kit 爬取 @%s 失败: %v\n输出: %s\n", username, err, outputStr)

			// 检测是否触发限速
			if strings.Contains(outputStr, "429") || strings.Contains(outputStr, "Too Many Requests") {
				fmt.Println("[!] 检测到 429 限速，暂停 2 分钟等待恢复...")
				time.Sleep(120 * time.Second)
			} else {
				time.Sleep(5 * time.Second)
			}
			continue
		}

		// 解析 JSON 输出
		type Tweet struct {
			ID           string `json:"id"`
			Text         string `json:"text"`
			CreatedAt    string `json:"createdAt"`
			PermanentURL string `json:"permanentUrl"`
			Username     string `json:"username"`
		}

		// Clean output: find the first '[' to ignore debug logs
		outputStr := string(output)
		startIndex := strings.Index(outputStr, "[")
		if startIndex == -1 {
			fmt.Printf("[!] x-kit 输出中未找到 JSON 数组: %s\n", outputStr)
			time.Sleep(5 * time.Second)
			continue
		}
		jsonPart := []byte(outputStr[startIndex:])

		var tweets []Tweet
		if err := json.Unmarshal(jsonPart, &tweets); err != nil {
			// 尝试解析错误信息，如果输出不是 JSON
			fmt.Printf("[!] 解析 @%s 的 x-kit 输出失败: %v\n原始输出: %s\n", username, err, string(output))
			time.Sleep(5 * time.Second)
			continue
		}

		for _, tweet := range tweets {
			// Twitter API 返回的时间格式通常是 "Mon Jan 02 15:04:05 +0000 2006" (RubyDate)
			// 但 x-kit 可能返回不同的格式，取决于 twitter-openapi-typescript 的处理
			// 假设它返回的是 API 的原始格式
			tweetTime, err := time.Parse(time.RubyDate, tweet.CreatedAt)
			if err != nil {
				// 尝试 RFC3339
				tweetTime, err = time.Parse(time.RFC3339, tweet.CreatedAt)
			}

			if err != nil {
				fmt.Printf("[!] 解析时间 '%s' 失败: %v\n", tweet.CreatedAt, err)
				continue
			}

			if !utils.IsIn24Hours(tweetTime) {
				continue
			}

			resultSlice = append(resultSlice, []string{tweet.PermanentURL, fmt.Sprintf("@%s: %s", tweet.Username, tweet.Text)})
		}
		// 正常请求间隔增加到 15 秒
		time.Sleep(15 * time.Second)
	}

	if len(resultSlice) == 0 {
		return nil, errors.New("x-kit 未找到24小时内记录")
	}

	return resultSlice, nil
}

// fetchWithAPIV2 使用官方API V2获取推文
func (x X) fetchWithAPIV2(token string) ([][]string, error) {
	client := &twitter.Client{
		Authorizer: &authorizer{
			Token: token,
		},
		Client: http.DefaultClient,
		Host:   "https://api.twitter.com",
	}

	var resultSlice [][]string
	targetUsers := getTargetUsers()
	fmt.Printf("[*] 使用 API V2 监控 %d 个用户账号\n", len(targetUsers))

	for _, username := range targetUsers {
		fmt.Printf("[*] 正在使用 API V2 爬取 @%s...\n", username)

		// 1. 通过用户名获取用户ID
		userResp, err := client.UserNameLookup(context.Background(), []string{username}, twitter.UserLookupOpts{})
		if err != nil {
			// Check for specific API errors returned in the response body
			if userResp != nil && len(userResp.Raw.Errors) > 0 {
				fmt.Printf("[!] 获取用户 @%s ID 失败: %s\n", username, userResp.Raw.Errors[0].Detail)
			} else {
				fmt.Printf("[!] 获取用户 @%s ID 失败: %v\n", username, err)
			}
			time.Sleep(2 * time.Second)
			continue
		}
		if len(userResp.Raw.Users) == 0 {
			fmt.Printf("[!] 未找到用户 @%s\n", username)
			time.Sleep(2 * time.Second)
			continue
		}
		userID := userResp.Raw.Users[0].ID

		// 2. 获取用户推文时间线
		opts := twitter.UserTweetTimelineOpts{
			TweetFields: []twitter.TweetField{twitter.TweetFieldCreatedAt, twitter.TweetFieldText},
			MaxResults:  10, // 获取最近10条
		}
		timeline, err := client.UserTweetTimeline(context.Background(), userID, opts)
		if err != nil {
			if timeline != nil && len(timeline.Raw.Errors) > 0 {
				fmt.Printf("[!] 获取 @%s 时间线失败: %s\n", username, timeline.Raw.Errors[0].Detail)
			} else {
				fmt.Printf("[!] 获取 @%s 时间线失败: %v\n", username, err)
			}
			time.Sleep(2 * time.Second)
			continue
		}

		// Check if there is any data
		if timeline.Raw == nil || len(timeline.Raw.Tweets) == 0 {
			fmt.Printf("[*] @%s 最近没有发布推文\n", username)
			time.Sleep(2 * time.Second)
			continue
		}

		dictionaries := timeline.Raw.TweetDictionaries()
		for _, tweetDict := range dictionaries {
			tweetTime, _ := time.Parse(time.RFC3339, tweetDict.Tweet.CreatedAt)
			if !utils.IsIn24Hours(tweetTime) {
				continue
			}
			permanentURL := fmt.Sprintf("https://twitter.com/%s/status/%s", username, tweetDict.Tweet.ID)
			resultSlice = append(resultSlice, []string{permanentURL, fmt.Sprintf("@%s: %s", username, tweetDict.Tweet.Text)})
		}
		time.Sleep(2 * time.Second)
	}

	if len(resultSlice) == 0 {
		return nil, errors.New("API V2 未找到24小时内记录")
	}

	return resultSlice, nil
}

// fetchWithScraper 使用免费爬虫获取推文（无需API）
func (x X) fetchWithScraper() ([][]string, error) {
	var resultSlice [][]string

	// 创建 scraper 实例
	scraper := twitterscraper.New()

	// 如果启用代理
	if config.Cfg.Proxy.CrawlerProxyEnabled {
		err := scraper.SetProxy(config.Cfg.Proxy.ProxyUrl)
		if err != nil {
			fmt.Printf("[!] 设置代理失败: %v\n", err)
		}
	}

	fmt.Printf("[*] [X] crawler result:\n%s\n\n", utils.CurrentTime())

	// 从配置获取目标用户列表
	targetUsers := getTargetUsers()
	fmt.Printf("[*] 监控 %d 个用户账号\n", len(targetUsers))

	// 遍历所有目标用户
	for _, username := range targetUsers {
		fmt.Printf("[*] 正在爬取 @%s...\n", username)

		// 获取用户推文
		count := 0
		for tweet := range scraper.GetTweets(context.Background(), username, 20) {
			if tweet.Error != nil {
				fmt.Printf("[!] 获取 @%s 推文失败: %v\n", username, tweet.Error)
				break
			}

			// 只收集24小时内的推文
			if !utils.IsIn24Hours(tweet.TimeParsed) {
				break
			}

			// 输出推文信息
			fmt.Println(tweet.TimeParsed.Format("2006/01/02 15:04:05"))
			fmt.Printf("@%s: %s\n", username, tweet.Text)
			fmt.Printf("%s\n\n", tweet.PermanentURL)

			// 添加到结果集
			var s []string
			s = append(s, tweet.PermanentURL, fmt.Sprintf("@%s: %s", username, tweet.Text))
			resultSlice = append(resultSlice, s)

			count++
			if count >= 5 { // 每个用户最多取5条最新推文
				break
			}
		}

		// 避免请求过快被限制
		time.Sleep(2 * time.Second)
	}

	if len(resultSlice) == 0 {
		return nil, errors.New("no records in the last 24 hours")
	}

	return resultSlice, nil
}
