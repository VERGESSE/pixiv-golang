package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"pixivic/pixiv"
	"pixivic/pixiv/strategy"

	"golang.org/x/net/proxy"
)

func main() {

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	picChan := make(chan *pixiv.PicDetail, 200)
	countdown := sync.WaitGroup{}
	done := make(chan bool)
	memo := make(map[string]bool)
	dialer, _ := proxy.SOCKS5("tcp", "127.0.0.1:1080",
		nil, &net.Dialer {
			Timeout: 30 * time.Second,
			KeepAlive: 30 * time.Second,})
	trans := &http.Transport{
		Dial: dialer.Dial,
	}
	client := &http.Client{
		Transport: trans,
		Timeout:   time.Second * 30, //超时时间
	}
	// 获取Cookie
	cookie := getCookie()
	p := &pixiv.Pixiv{
		GoroutinePool: make(chan struct{}, 200),   // 设置线程数量
		PicChan: picChan,						  // 存储图片id的通道
		Client: client,                           // http请求代理客户端
		Cookie: cookie,
		CountDown: &countdown,                    // 控制程序平稳结束的栅栏
		Memo: memo,                               // 缓存，防止下载重复图片
		Done: done,                               // 如果主动停止程序，依靠Done通知其他协程结束任务
		CrawlStrategy: strategy.KeywordStrategy,
	}
	// 加载缓存，防止下载之前的重复图片
	getOldImg(memo)

	fmt.Println("具体操作详见博客: https://www.vergessen.top/article/v/9942142761049736")
	fmt.Println("默认输入关键字爬取关键字对应的收藏数大于1000的图片")
	input := bufio.NewScanner(os.Stdin)
	var inputCtx string
	if input.Scan() {
		inputCtx = strings.ToLower(input.Text())
	}

	if initPixiv(p, inputCtx) {
		// 设置输入任意字符退出,如回车
		go func() {
			for {
				if input.Scan() {
					scan := strings.ToLower(input.Text())
					if scan == "q" {
						fmt.Println("停止进程中, 程序将在执行完已提交任务后退出...")
						done <- true
						p.PicChan <- &pixiv.PicDetail{}
						break
					}
				}
			}
		}()

		// 开启根据关键词下载策略
		p.GetUrls()

		// 开启图片下载任务
		p.CrawUrl()

		// 等待已经启动的任务结束
		countdown.Wait()
	} else {
		fmt.Println("输入参数有误！")
	}

	fmt.Println()
	fmt.Println("进程已停止, 按回车退出程序...")
	fmt.Println()
	input.Scan()
}

func initPixiv(p *pixiv.Pixiv, inputCtx string) bool {
	inputCtx = strings.Trim(strings.ToLower(inputCtx), " ")
	keywords := strings.Split(inputCtx, " ")
	if len(keywords) == 0 {
		return false
	}
	if keywords[0] == "all" {
		keywords[0] = ""
	}
	p.KeyWord = keywords[0]
	p.Bookmarks = 1000
	p.PicType = "wh"
	for _, keyword := range keywords[1:] {
		switch keyword[:2] {
		case "-b":
			bookmarks, err := strconv.Atoi(keyword[2:])
			if err != nil {
				return false
			}
			p.Bookmarks = bookmarks
		case "-t":
			p.PicType = keyword[2:]
		case "-s":
			switch keyword[2:] {
			case "keyword":
				p.CrawlStrategy = strategy.KeywordStrategy
				fmt.Println("即将根据搜索关键字爬取图片")
			case "related":
				if _, err := strconv.Atoi(keywords[0]); err != nil {
					return false
				}
				p.CrawlStrategy = strategy.PicIdStrategy
				fmt.Println("即将根据图片ID爬取相关图片")
			case "author":
				if _, err := strconv.Atoi(keywords[0]); err != nil {
					return false
				}
				p.CrawlStrategy = strategy.AuthorStrategy
				fmt.Println("即将根据作者ID爬取该作者的所有图片")
			default:
				p.CrawlStrategy = strategy.KeywordStrategy
				fmt.Println("即将根据搜索关键字爬取图片")
			}
		}
	}
	return true
}

// 获取之前下载的缓存的函数, images/memos 缓存了曾经所有下载过的图片的id，以空格分隔
func getOldImg(memo map[string]bool) {
	os.MkdirAll("images",0644)
	memoFile, _ := os.OpenFile("images/memos",
		os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	reader := bufio.NewReader(memoFile)
	for {
		s, e := reader.ReadString(byte(' '))
		if e != nil {
			break
		}
		memo[strings.Split(s," ")[0]] = true
	}
	memoFile.Close()
}

func getCookie() string {
	cookieFile, _ := os.OpenFile("cookie.txt",
		os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	reader := bufio.NewReader(cookieFile)
	line, _, _ := reader.ReadLine()
	cookie := fmt.Sprintf("%s", line)
	return cookie[:len(cookie)-1]
}

