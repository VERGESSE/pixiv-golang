package urlcrawler

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// 访问初始地址 + 图片ID即可获取图片信息
	baseUrl string = "https://api.pixivic.com/illusts/"
	// 只下载插画
	tag string = "illust"
)

// 爬虫结构体
type Pixivic struct {
	GoroutinePool chan struct{}
	PicChan       chan *PicDetail
	CountDown     *sync.WaitGroup
	Memo          map[string]bool
	Done          chan bool
	// 爬取关键字
	KeyWord string
	// 要求点赞数 默认 1000
	Bookmarks int
	// 爬取的图片类型 W: 横屏 H: 竖屏 S: 小屏 O:其他(默认WH)
	PicType string
	// 负责向 PicChan 提供封装好的图片信息
	CrawlStrategy func(p *Pixivic)
	// 是否取消任务
	IsCancel int32
}

// 存储爬取图片原始信息的结构体
type UrlDetail struct {
	Data []OriginalDetail
}

// 图片原始信息
type OriginalDetail struct {
	Id   int
	Type string
	// 图片地址
	ImageUrls []struct {
		Original string
	}
	// 创建时间
	CreateDate string
	// 图片宽度，高度，收藏数
	Width, Height, TotalBookmarks int
}

// 爬取图片的具体信息
type PicDetail struct {
	Id  string
	Url string
	// 宽高比
	Ratio float32
	// 图片类型，横屏，竖屏（直接对应存储的文件名）
	Group string
}

// 传入Id的相关页面信息
type relevancePage struct {
	Data []struct {
		Id int
	}
}

// 向文件写入缓存的任务通道
var cacheChan = make(chan string, 20)

// 分发下载任务
func (p *Pixivic) CrawUrl() {

	var index int64 = 1
	// 开启缓存任务
	go addCache()

	// 从图片ID通道读取图片ID并开启一个协程下载
	for pic := range p.PicChan {
		// 判断是否取消任务
		if p.cancelled() {
			// 标记取消任务
			atomic.AddInt32(&p.IsCancel, 1)
			break
		}
		imgId := pic.Id
		// 判断是否下载过
		if !p.Memo[imgId] {
			p.Memo[imgId] = true
			// 从池中申请一个协程，开启任务
			p.GoroutinePool <- struct{}{}
			// 任务计数加一
			p.CountDown.Add(1)
			go func(detail *PicDetail) {
				start := time.Now()
				// 根据ID下载图片, isDown代表下载成功或者失败
				isDown := downloadImg(detail)
				// 如果下载成功则通知缓存通道向momes中添加已经下载图片的ID
				// 然后通知用户图片下载成功以及用时
				if isDown {
					// 通知缓存队列
					cacheChan <- detail.Id
					fmt.Println(index, ": ", detail.Id, " 爬取成功 !",
						time.Since(start), " 输入 q 退出...")
					// 使用原子递增保证线程安全
					atomic.AddInt64(&index, 1)
				}
				// 正在运行任务数减一，并向池中归还协程
				p.CountDown.Done()
				<-p.GoroutinePool
			}(pic)
		}
	}
	// 关闭线程池
	close(p.GoroutinePool)
	// 等待任务全部完成,关闭缓存队列
	p.CountDown.Wait()
	close(cacheChan)
	close(p.Done)
}

// 根据输入关键字获取图片id
func (p *Pixivic) GetUrls() {

	// 把keyword转成浏览器可用16进制
	p.KeyWord = url.QueryEscape(p.KeyWord)
	go func() {
		p.CrawlStrategy(p)
		// 优雅关闭
		if atomic.LoadInt32(&p.IsCancel) == 0 {
			p.Done <- true
			p.PicChan <- &PicDetail{}
		}
	}()
}

// 根据传入图片Id下载图片
func downloadImg(detail *PicDetail) bool {
	referUrl := baseUrl + detail.Id

	client := http.Client{}
	// 拼接图片地址URL
	pictureUrl := &url.URL{}
	pictureUrl, _ = pictureUrl.Parse(detail.Url)
	// 返回的图片地址和真正的域名有差异，需要改变域名部分
	pictureUrl.Host = "original.img.cheerfun.dev"

	// 设置http请求地址，如果不设置referer，将返回403页面
	header := &http.Header{}
	header.Add("referer", referUrl)
	header.Add("user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/80.0.3987.149 Safari/537.36")

	request := &http.Request{
		Method: "GET",
		URL:    pictureUrl,
		Header: *header,
	}
	// 下载图片
	resp, err := client.Do(request)
	if err != nil {
		log.Println(err)
		return false
	}
	defer resp.Body.Close()

	picName := detail.Id + strings.Split(detail.Url, "_")[1][2:]

	// 根据图片的尺寸信息确定图片归属
	bathPath := "images/" + detail.Group + "/"
	// 创建图片目录
	os.MkdirAll(bathPath, 0644)
	file, e := os.OpenFile(bathPath+picName, os.O_RDWR|os.O_CREATE, 0644)
	if e != nil {
		log.Println(err)
		return false
	}
	defer file.Close()
	// 将图片下载的流直接对接到相应文件
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		file.Close()
		os.Remove(bathPath + picName)
		return false
	}
	// 如果下载出现问题则删除文件
	info, err := file.Stat()
	if err != nil {
		file.Close()
		os.Remove(bathPath + picName)
		return false
	}
	if info.Size() < 100 {
		file.Close()
		os.Remove(bathPath + picName)
		return false
	}
	return true
}

// 向缓存文件写入新下载的文件
func addCache() {
	file, _ := os.OpenFile("images/memos",
		os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	for imgId := range cacheChan {
		file.WriteString(imgId + " ")
	}
	file.Close()
}

// 判断任务是否结束
func (p *Pixivic) cancelled() bool {
	select {
	case <-p.Done:
		return true
	default:
		return false
	}
}
