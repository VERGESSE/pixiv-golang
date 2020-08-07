package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"pixivic/strategy"
	"pixivic/urlcrawler"
	"strconv"
	"strings"
	"sync"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	picChan := make(chan *urlcrawler.PicDetail, 10)
	countdown := sync.WaitGroup{}
	done := make(chan bool)
	memo := make(map[string]bool)
	pixivic := &urlcrawler.Pixivic{
		GoroutinePool: make(chan struct{}, 20),   // 设置线程数量
		PicChan: picChan,						  // 存储图片id的通道
		CountDown: &countdown,                    // 控制程序平稳结束的栅栏
		Memo: memo,                               // 缓存，防止下载重复图片
		Done: done,                               // 如果主动停止程序，依靠Done通知其他协程结束任务
		CrawlStrategy: strategy.KeywordStrategy,
	}
	// 加载缓存，防止下载之前的重复图片
	getOld(memo)

	fmt.Println("具体操作详见博客: https://www.vergessen.top/article/v/9942142761049735")
	fmt.Println("默认输入关键字爬取关键字对应的收藏数大于1000的图片")
	input := bufio.NewScanner(os.Stdin)
	var inputCtx string
	if input.Scan() {
		inputCtx = strings.ToLower(input.Text())
	}

	if initPixivic(pixivic, inputCtx) {
		// 设置输入任意字符退出,如回车
		go func() {
			for {
				if input.Scan() {
					scan := strings.ToLower(input.Text())
					if scan == "q" {
						fmt.Println("停止进程中, 程序将在执行完已提交任务后退出...")
						done <- true
						pixivic.PicChan <- &urlcrawler.PicDetail{}
						break
					}
				}
			}
		}()

		// 启动根据输入Id，加载相关图片的协程, 并递归调用一层
		//go pixivic.GetRelevanceUrls(originId[0], true, 6)

		// 开启根据关键词下载策略
		pixivic.GetUrls()

		// 开启图片下载任务
		pixivic.CrawUrl()

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

func initPixivic(p *urlcrawler.Pixivic, inputCtx string) bool {
	keywords := strings.Split(inputCtx, " ")
	if len(keywords) == 0 {
		return false
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
func getOld(memo map[string]bool) {
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
