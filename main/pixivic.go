package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"pixivic/urlcrawler"
	"strconv"
	"strings"
	"sync"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	idChan := make(chan string, 10)
	countdown := sync.WaitGroup{}
	done := make(chan bool)
	memo := make(map[string]bool)
	pixivic := &urlcrawler.Pixivic{
		GoroutinePool: make(chan struct{}, 30),   // 设置协程数量
		IdChan: idChan,							  // 存储图片id的通道
		CountDown: &countdown,                    // 控制程序平稳结束的栅栏
		Memo: memo,                               // 缓存，防止下载重复图片
		Done: done,                               // 如果主动停止程序，依靠Done通知其他协程结束任务
	}
	// 加载缓存，防止下载之前的重复图片
	getOld(memo)

	fmt.Println("请输入起始地址(起始地址请访问:https://pixivic.com)以及最低收藏数(默认800)")
	fmt.Println("选择一张你喜欢的图片，点进去并复制地址")
	fmt.Println("例如:https://pixivic.com/illusts/76701981?VNK=35fda4b2?>2000")
	fmt.Println("或者直接输入起始图片的id,例如: 76701981?>2000")
	fmt.Println("或者直接输入图片ID:76701981,默认爬取收藏大于800的图片")
	input := bufio.NewScanner(os.Stdin)
	var originUrl string
	if input.Scan() {
		originUrl = input.Text()
	}
	// 根据输入获取起始地址
	originId := strings.Split(originUrl, "/")
	originId = strings.Split(originId[len(originId)-1], "?")
	// 根据输入获取最低点赞数
	split := strings.Split(originUrl, ">")
	if len(split) > 1 {
		bookmarks := split[1]
		pixivic.Bookmarks, _ = strconv.Atoi(bookmarks)
	} else {
		pixivic.Bookmarks = 800
	}

	// 设置输入任意字符退出,如回车
	go func() {
		input.Scan()
		fmt.Println("停止进程中, 程序将在执行完已提交任务后退出...")
		done <- true
	}()

	// 启动根据输入Id，加载相关图片的协程, 并递归调用一层
	go pixivic.GetRelevanceUrls(originId[0], true, 6)
	// 开启图片下载任务
	pixivic.CrawUrl()

	// 等待已经启动的任务结束
	countdown.Wait()
	fmt.Println("进程已停止, 按回车退出程序...")
	input.Scan()
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
