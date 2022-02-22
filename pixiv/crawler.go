package pixiv

import (
	"fmt"
	"io"
	"log"
	"math/rand"
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
	baseUrl  string = "https://i.pximg.net/img-original/img/"
	referUrl string = "https://www.pixiv.net/artworks/"
)

// 收藏数可选项
var Bookmark = [8]int{0, 50, 100, 300, 500, 1000, 5000, 10000}

// 爬虫结构体
type Pixiv struct {
	GoroutinePool chan struct{}
	PicChan       chan *PicDetail
	RequestPool   chan struct{}
	CountDown     *sync.WaitGroup
	Memo          map[string]bool
	Done          chan bool
	// http请求代理客户端
	Client *http.Client
	// Cookie
	Cookie string
	// 爬取关键字
	KeyWord string
	// 要求点赞数 默认 1000
	Bookmarks int
	// 爬取的图片类型 W: 横屏 H: 竖屏 S: 小屏 O:其他(默认WH)
	PicType string
	// 负责向 PicChan 提供封装好的图片信息
	CrawlStrategy func(p *Pixiv)
	// 是否取消任务
	IsCancel int32
	// 并发控制
	Mutex *sync.Mutex
}

// 存储爬取图片原始信息的结构体
type UrlDetail struct {
	Body struct {
		Illust struct {
			Data  []Illust
			Total int
		}
	}
}

type UrlDetail2 struct {
	Body struct {
		Illusts []Illust
	}
}

// 图片原始信息
type Illust struct {
	Id   string
	Type string
	// 图片地址
	Url  string
	Tags []string
	// 创建时间
	CreateDate string
	// 图片宽度，高度，收藏数
	Width, Height, BookmarkData int
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

// 向文件写入缓存的任务通道
var cacheChan = make(chan string, 20)

// 分发下载任务
func (p *Pixiv) CrawUrl() {

	var index int64 = 1
	// 开启缓存任务
	go addCache()

	var numAll int64 = 0
	var numDown int64 = 0
	// 从图片ID通道读取图片ID并开启一个协程下载
	for pic := range p.PicChan {
		// 判断是否取消任务
		if p.Cancelled() {
			// 标记取消任务
			atomic.AddInt32(&p.IsCancel, 1)
			break
		}
		imgId := pic.Id
		numAll++
		//if numAll%100 == 0 {
		//	fmt.Println("下载率(", len(p.Memo), "):", 100*float64(numDown)/float64(numAll), "%")
		//}
		// 判断是否下载过
		p.Mutex.Lock()
		if !p.Memo[imgId] {
			p.Memo[imgId] = true
			p.Mutex.Unlock()
			numDown++
			// 从池中申请一个协程，开启任务
			p.GoroutinePool <- struct{}{}
			// 任务计数加一
			p.CountDown.Add(1)
			go func(detail *PicDetail) {
				start := time.Now()
				// 根据ID下载图片, isDown代表下载成功或者失败
				isDown := p.downloadImg(detail)
				// 如果下载成功则通知缓存通道向memos中添加已经下载图片的ID
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
		} else {
			p.Mutex.Unlock()
		}
	}
	// 关闭线程池
	close(p.GoroutinePool)
	close(p.RequestPool)
	// 等待任务全部完成,关闭缓存队列
	p.CountDown.Wait()
	close(cacheChan)
	close(p.Done)
}

// 根据输入关键字获取图片id
func (p *Pixiv) GetUrls() {

	// 把keyword转成浏览器可用16进制
	p.KeyWord = url.QueryEscape(p.KeyWord)
	go func() {
		p.CrawlStrategy(p)
		// 优雅关闭
		if atomic.LoadInt32(&p.IsCancel) == 0 {
			p.PicChan <- &PicDetail{}
			p.Done <- true
			p.PicChan <- &PicDetail{}
		}
	}()
}

// 根据传入图片Id下载图片
func (p *Pixiv) downloadImg(detail *PicDetail) bool {
	referUrl := referUrl + detail.Id

	// 拼接图片地址URL
	originalUrl := detail.Url
	if len(originalUrl) == 0 {
		return false
	}
	secondUrl := strings.Split(originalUrl, "/img/")[1]
	imgDateId := strings.Split(secondUrl, "_")[0]
	imgType := strings.Split(secondUrl, ".")[1]
	endUrl := baseUrl + imgDateId + "_p0." + imgType

	pictureUrl := &url.URL{}
	pictureUrl, _ = pictureUrl.Parse(endUrl)

	// 设置http请求地址，如果不设置referer，将返回403页面
	header := &http.Header{}
	header.Add("referer", referUrl)
	header.Add("user-agent", GetRandomUserAgent())

	request := &http.Request{
		Method: "GET",
		URL:    pictureUrl,
		Header: *header,
	}
	// 下载图片
	client := p.Client
	resp, err := client.Do(request)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	picName := detail.Id + "." + imgType

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
		detail.Url = originalUrl[:len(originalUrl)-3] + "png"
		return p.downloadImg(detail)
	}
	return true
}

// http请求 进行并发度控制
func (p *Pixiv) DoRequest(req *http.Request) (*http.Response, error) {
	p.RequestPool <- struct{}{}
	response, e := p.Client.Do(req)
	<-p.RequestPool
	return response, e
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
func (p *Pixiv) Cancelled() bool {
	select {
	case <-p.Done:
		return true
	default:
		return false
	}
}

func GetRandomUserAgent() string {
	index := rand.Int() % len(userAgent)
	return userAgent[index]
}

var userAgent = [...]string{
	"Mozilla/5.0 (Windows NT 6.1; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/39.0.2171.95 Safari/537.36 OPR/26.0.1656.60",
	"Mozilla/5.0 (Windows NT 5.1; U; en; rv:1.8.1) Gecko/20061208 Firefox/2.0.0 Opera 9.50",
	"Mozilla/5.0 (Windows NT 6.1; WOW64; rv:34.0) Gecko/20100101 Firefox/34.0",
	"Mozilla/5.0 (X11; U; Linux x86_64; zh-CN; rv:1.9.2.10) Gecko/20100922 Ubuntu/10.10 (maverick) Firefox/3.6.10",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/75.0.3770.142 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:69.0) Gecko/20100101 Firefox/69.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/76.0.3809.100 Safari/537.36 OPR/63.0.3368.43",
	"User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/70.0.3538.102 Safari/537.36 Edge/18.18362",
	"User-Agent: Mozilla/5.0 (Windows NT 10.0; WOW64; Trident/7.0; LCTE; rv:11.0) like Gecko",
	"Mozilla/5.0 (Windows NT 10.0; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/58.0.3029.110 Safari/537.36 SE 2.X MetaSr 1.0",
	"Mozilla/5.0 (Windows NT 10.0; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/55.0.2883.87 UBrowser/6.2.3964.2 Safari/537.36",
	"Mozilla/5.0 (Windows NT 6.2; WOW64) AppleWebKit/534.54.16 (KHTML, like Gecko) Version/5.1.4 Safari/534.54.16",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/80.0.3987.149 Safari/537.36",
}
