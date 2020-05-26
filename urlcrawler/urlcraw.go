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

// 传入Id的相关页面信息
type relevancePage struct {
	Data []struct {
		Id int
	}
}

// 最低收藏数
var bookmarks int

// 向文件写入缓存的任务通道
var cacheChan = make(chan string, 20)

// 分发下载任务
func (p *Pixivic) CrawUrl() {

	// 初始化最低收藏数
	bookmarks = p.Bookmarks

	index := 1
	// 开启缓存任务
	go addCache()

	// 从图片ID通道读取图片ID并开启一个协程下载
	for imgId := range p.IdChan {
		// 判断是否下载过
		if !p.Memo[imgId] {
			p.Memo[imgId] = true
			// 从池中申请一个协程，开启任务
			p.GoroutinePool <- struct{}{}
			// 任务计数加一
			p.CountDown.Add(1)
			go func(imgId string) {
				start := time.Now()
				// 根据ID下载图片, isDown代表下载成功或者失败
				isDown := downloadImg(imgId)
				// 如果下载成功则通知缓存通道向momes中添加已经下载图片的ID
				// 然后通知用户图片下载成功以及用时
				if isDown {
					// 通知缓存队列
					cacheChan <- imgId
					fmt.Println(index, ": ", imgId, " 爬取成功 !",
						time.Since(start), " 按回车退出...")
					index++
				}
				// 正在运行任务数减一，并向池中归还协程
				p.CountDown.Done()
				<-p.GoroutinePool
			}(imgId)
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
	referUrl := baseUrl + imgId

	// 获取ID对应图片的初始信息
	resp, err := http.Get(referUrl)
	if err != nil {
		log.Println(err)
		return false
	}
	var detail = &urlDetail{}
	json.NewDecoder(resp.Body).Decode(detail)
	resp.Body.Close()

	//bytes, _ := json.MarshalIndent(detail, "", "  ")
	//fmt.Printf("%s\n", bytes)
	data := detail.Data
	urls := data.ImageUrls
	if len(urls) < 1 || data.TotalBookmarks < bookmarks || data.Type != tag {
		return false
	}
	// 拼接图片地址URL
	pictureUrl := &url.URL{}
	pictureUrl, _ = pictureUrl.Parse(urls[0].Original)
	// 返回的图片地址和真正的域名有差异，需要改变域名部分
	pictureUrl.Host = "original.img.cheerfun.dev"

	// 根据ID计算图片的名称
	nameSlice := strings.Split(urls[0].Original, "/")
	pictureName := strings.Split(nameSlice[len(nameSlice)-1], "_")[0] + "." +
		strings.Split(nameSlice[len(nameSlice)-1], ".")[1]

	// 设置http请求地址，如果不设置referer，将返回403页面
	header := &http.Header{}
	header.Add("referer", "https://pixivic.com/illusts/"+imgId)
	header.Add("user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/80.0.3987.149 Safari/537.36")
	client := http.Client{}
	request := &http.Request{
		Method: "GET",
		URL:    pictureUrl,
		Header: *header,
	}
	// 下载图片
	resp, err = client.Do(request)
	if err != nil {
		log.Println(err)
		return false
	}
	defer resp.Body.Close()

	// 计算图片的宽高比
	radio := float64(data.Width) / float64(data.Height)
	// 根据图片的尺寸信息确定图片归属
	bathPath := "images/"
	if data.Width >= 1800 && (radio < 2.33 || radio > 1.52) {
		bathPath += "横屏/"
	} else if data.Height >= 1800 && (radio > 0.46 || radio < 0.77) {
		bathPath += "竖屏/"
	} else if data.Width >= 1800 || data.Height >= 1800 {
		bathPath += "长图"
	} else {
		bathPath += "小图/"
	}
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

// 获取图片Id的相关图片
func (p *Pixivic) GetRelevanceUrls(imgId string) {
	p.IdChan <- imgId
	for i := 1; i <= 10; i++ {
		originUrl := "https://api.pixivic.com/illusts/" +
			imgId + "/related?page=" + strconv.Itoa(i) + "&pageSize=200"

		resp, err := http.Get(originUrl)
		if err != nil {
			log.Println(err)
		}
		var relevancePage = &relevancePage{}
		json.NewDecoder(resp.Body).Decode(relevancePage)
		resp.Body.Close()

		if i > 0 {
			if len(relevancePage.Data) > 0 {
				imgId = strconv.Itoa(relevancePage.Data[0].Id)
				i = 0
			}
		}

		// 向图片ID通道添加任务
		for _, id := range relevancePage.Data {
			// 如果任务取消，退出
			if p.cancelled() {
				close(p.IdChan)
				close(p.Done)
				return
			}
			p.IdChan <- strconv.Itoa(id.Id)
		}
	}
	close(p.IdChan)
	close(p.Done)
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
