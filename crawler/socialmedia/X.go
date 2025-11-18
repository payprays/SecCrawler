package socialmedia

import (
	. "SecCrawler/config"
	"SecCrawler/register"
	"SecCrawler/utils"
	"context"
	"errors"
	"fmt"
	"time"

	twitterscraper "github.com/n0madic/twitter-scraper"
)

type X struct{}

// getTargetUsers 从配置文件获取目标用户列表
func getTargetUsers() []string {
	return Cfg.Crawler.SocialMedia.X.IDs
}

func (x X) Config() register.CrawlerConfig {
	return register.CrawlerConfig{
		Name:        "SocialMedia.X",
		Description: "X(Twitter)平台安全情报聚合",
	}
}

// Get 获取X平台前24小时内推文
func (x X) Get() ([][]string, error) {
	key, secret := Cfg.Crawler.SocialMedia.X.Key, Cfg.Crawler.SocialMedia.X.Secret

	if key != "" && secret != "" {
		fmt.Println("[*] 尝试使用 Twitter API...")
		tweets, err := x.fetchWithAPI(key, secret)
		if err != nil {
			fmt.Printf("[!] API 调用失败: %v，切换到免费方案...\n", err)
			return x.fetchWithScraper()
		}
		return tweets, nil
	}

	fmt.Println("[*] 未配置 API，使用免费爬虫方案...")
	return x.fetchWithScraper()
}

// fetchWithAPI 使用官方API获取推文（需要配置API密钥）
func (x X) fetchWithAPI(key, secret string) ([][]string, error) {
	// TODO: 实现官方API调用
	// 这里需要使用 go-twitter 库，但需要更复杂的OAuth认证
	// 暂时返回错误，让其回退到免费方案
	return nil, errors.New("API authentication not implemented yet")
}

// fetchWithScraper 使用免费爬虫获取推文（无需API）
func (x X) fetchWithScraper() ([][]string, error) {
	var resultSlice [][]string

	// 创建 scraper 实例
	scraper := twitterscraper.New()

	// 如果启用代理
	if Cfg.Proxy.CrawlerProxyEnabled {
		err := scraper.SetProxy(Cfg.Proxy.ProxyUrl)
		if err != nil {
			fmt.Printf("[!] 设置代理失败: %v\n", err)
		}
	}

	fmt.Printf("[*] [X] crawler result:\n%s\n\n", utils.CurrentTime())

	// 创建 context
	ctx := context.Background()

	// 从配置获取目标用户列表
	targetUsers := getTargetUsers()
	fmt.Printf("[*] 监控 %d 个用户账号\n", len(targetUsers))

	// 遍历所有目标用户
	for _, username := range targetUsers {
		fmt.Printf("[*] 正在爬取 @%s...\n", username)

		// 获取用户推文
		count := 0
		for tweet := range scraper.GetTweets(ctx, username, 20) {
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
