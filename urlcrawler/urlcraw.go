package urlcrawler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
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
	IdChan        chan string
	CountDown     *sync.WaitGroup
	Memo          map[string]bool
	Done          chan bool
	Bookmarks     int
}

// 传入图片id的详情信息
type urlDetail struct {
	Data struct {
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
}

// 使用关键字时的存储结构体
type keywordDetails struct {
	Data []struct {
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
}

// 传入Id的相关页面信息
type relevancePage struct {
	Data []struct {
		Id int
	}
}

// 最低收藏数
var bookmarks int

// 记录是否关闭任务，原子变量
var isCancel int32 = 0

// 向文件写入缓存的任务通道
var cacheChan = make(chan string, 20)

// 记录图片id和宽高比的map
var ratioMap = make(map[int]bool)

// 分发下载任务
func (p *Pixivic) CrawUrl() {

	// 初始化最低收藏数
	bookmarks = p.Bookmarks

	var index int64 = 1
	// 开启缓存任务
	go addCache()

	// 从图片ID通道读取图片ID并开启一个协程下载
	for imgUrl := range p.IdChan {
		// 判断是否取消任务
		if p.cancelled() {
			// 标记取消任务
			atomic.AddInt32(&isCancel, 1)
			break
		}
		nameSlice := strings.Split(imgUrl, "/")
		imgId := strings.Split(nameSlice[len(nameSlice)-1], "_")[0]
		// 判断是否下载过
		if !p.Memo[imgId] {
			p.Memo[imgId] = true
			// 从池中申请一个协程，开启任务
			p.GoroutinePool <- struct{}{}
			// 任务计数加一
			p.CountDown.Add(1)
			go func(imgUrl, imgId string) {
				start := time.Now()
				// 根据ID下载图片, isDown代表下载成功或者失败
				isDown := downloadImg(imgUrl)
				// 如果下载成功则通知缓存通道向momes中添加已经下载图片的ID
				// 然后通知用户图片下载成功以及用时
				if isDown {
					// 通知缓存队列
					cacheChan <- imgId
					fmt.Println(index, ": ", imgId, " 爬取成功 !",
						time.Since(start), " 按回车退出...")
					// 使用原子递增保证线程安全
					atomic.AddInt64(&index, 1)
				}
				// 正在运行任务数减一，并向池中归还协程
				p.CountDown.Done()
				<-p.GoroutinePool
			}(imgUrl, imgId)
		}
	}
	// 关闭线程池
	close(p.GoroutinePool)
	// 等待任务全部完成,关闭缓存队列
	p.CountDown.Wait()
	close(cacheChan)
}

// 根据传入图片Id下载图片
func downloadImg(imgId string) bool {
	//referUrl := baseUrl + imgId

	client := http.Client{}
	//// 获取ID对应图片的初始信息
	//request, _ := http.NewRequest(http.MethodGet, referUrl, nil)
	//request.Header.Add("authorization","eyJhbGciOiJIUzUxMiJ9.eyJwZXJtaXNzaW9uTGV2ZWwiOjEsInJlZnJlc2hDb3VudCI6MSwiaXNCYW4iOjEsInVzZXJJZCI6MTc1OTM2LCJpYXQiOjE1OTI1NTY5NDgsImV4cCI6MTU5NDI4NDk0OH0.O4q5yNf9ln9CFH-gb4jAuiq6D1nwWP6bA_fdkKgb2sZlqWTDFk7bAlUqWhAS8-jJ3uI8_zFKs6hWsgyLfxIt4A")
	//request.Header.Add("user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/80.0.3987.149 Safari/537.36")
	//resp, err := client.Do(request)
	//if err != nil {
	//	log.Println(err)
	//	return false
	//}
	//var detail = &urlDetail{}
	//json.NewDecoder(resp.Body).Decode(detail)
	////fmt.Println(detail)
	//resp.Body.Close()
	//
	////bytes, _ := json.MarshalIndent(detail, "", "  ")
	////fmt.Printf("%s\n", bytes)
	//data := detail.Data
	//urls := data.ImageUrls
	//if len(urls) < 1 || data.TotalBookmarks < bookmarks || data.Type != tag {
	//	return false
	//}
	// 拼接图片地址URL
	pictureUrl := &url.URL{}
	pictureUrl, _ = pictureUrl.Parse(imgId)
	// 返回的图片地址和真正的域名有差异，需要改变域名部分
	pictureUrl.Host = "original.img.cheerfun.dev"

	// 根据ID计算图片的名称
	//nameSlice := strings.Split(urls[0].Original, "/")
	//pictureName := strings.Split(nameSlice[len(nameSlice)-1], "_")[0] + "." +
	//	strings.Split(nameSlice[len(nameSlice)-1], ".")[1]
	nameSlice := strings.Split(imgId, "/")
	imgId = strings.Split(nameSlice[len(nameSlice)-1], "_")[0]
	pictureName := strings.Split(nameSlice[len(nameSlice)-1], "_")[0] + "." +
		strings.Split(nameSlice[len(nameSlice)-1], ".")[1]

	// 设置http请求地址，如果不设置referer，将返回403页面
	header := &http.Header{}
	header.Add("referer", "https://www.pixivic.com/illusts/"+imgId)
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

	// 计算图片的宽高比
	//ratio := float64(data.Width) / float64(data.Height)
	id, _ := strconv.Atoi(imgId)
	isWidth := ratioMap[id]
	delete(ratioMap, id)
	// 根据图片的尺寸信息确定图片归属
	bathPath := "images/"
	if isWidth {
		bathPath += "横屏/"
	} else {
		bathPath += "竖屏/"
	}
	//if data.Width >= 1800 && (ratio < 2.33 && ratio > 1.4) {
	//	bathPath += "横屏/"
	//} else if data.Height >= 1800 && (ratio > 0.46 && ratio < 0.8) {
	//	bathPath += "竖屏/"
	//} else if data.Width >= 1800 || data.Height >= 1800 {
	//	bathPath += "长图-方图/"
	//	return false
	//} else {
	//	bathPath += "小图/"
	//	return false
	//}
	// 创建图片目录
	os.MkdirAll(bathPath, 0644)
	file, e := os.OpenFile(bathPath+pictureName, os.O_RDWR|os.O_CREATE, 0644)
	if e != nil {
		log.Println(err)
		return false
	}
	defer file.Close()
	// 将图片下载的流直接对接到相应文件
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		file.Close()
		os.Remove(bathPath + pictureName)
		return false
	}
	// 如果下载出现问题则删除文件
	info, err := file.Stat()
	if err != nil {
		file.Close()
		os.Remove(bathPath + pictureName)
		return false
	}
	if info.Size() < 100 {
		file.Close()
		os.Remove(bathPath + pictureName)
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

// 五个生产者
var urlChan = make(chan struct{}, 5)

// 记录缓存的层爬取过的主页
var pageCache = &sync.Map{}

// 根据输入关键字获取图片id
func (p *Pixivic) GetKeywordsUrls(keyword string) {

	// 把keyword转成浏览器可用16进制
	keyword = url.QueryEscape(keyword)

	for i := 1; i <= 100; i++ {
		resp, err := http.Get("https://api.pixivic.com/illustrations?" +
			"illustType=illust&searchType=original&maxSanityLevel=9&page=" + strconv.Itoa(i) + "&keyword=" + keyword + "&pageSize=30")
		if err != nil {
			log.Println(err)
		}
		var details = &keywordDetails{}
		json.NewDecoder(resp.Body).Decode(details)
		var isWidth bool
		for _, detail := range details.Data {
			// 计算宽高比
			var ratio float64
			if detail.Height > detail.Width {
				if detail.Height < 1800 {
					continue
				}
				isWidth = false
				ratio = float64(detail.Height) / float64(detail.Width)
			} else {
				if detail.Width < 1800 {
					continue
				}
				isWidth = true
				ratio = float64(detail.Width) / float64(detail.Height)
			}

			// 判断是否加入下载队列
			if detail.TotalBookmarks >= p.Bookmarks && ratio < 2.33 && ratio > 1.4 {
				if atomic.LoadInt32(&isCancel) == 0 {
					p.IdChan <- detail.ImageUrls[0].Original
					ratioMap[detail.Id] = isWidth
				}
			}
		}
	}
}

// 获取图片Id的相关图片
func (p *Pixivic) GetRelevanceUrls(imgId string, recursion bool, index int) {
	urlChan <- struct{}{}
	p.IdChan <- imgId
	for i := 1; i <= 3; i++ {
		originUrl := "https://api.pixivic.com/illusts/" +
			imgId + "/related?page=" + strconv.Itoa(i) + "&pageSize=60"

		resp, err := http.Get(originUrl)
		if err != nil {
			log.Println(err)
			break
		}
		var relevancePage = &relevancePage{}
		json.NewDecoder(resp.Body).Decode(relevancePage)
		resp.Body.Close()

		// 向图片ID通道添加任务
		for _, id := range relevancePage.Data {
			curId := strconv.Itoa(id.Id)
			if atomic.LoadInt32(&isCancel) == 0 {
				p.IdChan <- curId
				if index == 0 {
					recursion = false
				}
				if v, _ := pageCache.Load(curId); v == nil {
					pageCache.Store(curId, true)
					// 只递归爬取index层
					go p.GetRelevanceUrls(curId, recursion, index-1)
				}
			}
		}
	}
	if !recursion && atomic.LoadInt32(&isCancel) == 0 {
		atomic.AddInt32(&isCancel, 1)
		close(p.IdChan)
	}
	<-urlChan
}

var lock sync.Mutex

// 判断任务是否结束
func (p *Pixivic) cancelled() bool {
	lock.Lock()
	defer lock.Unlock()
	select {
	case <-p.Done:
		return true
	default:
		return false
	}
}
