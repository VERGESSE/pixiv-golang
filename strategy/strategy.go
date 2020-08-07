package strategy

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"

	pixivic "pixivic/urlcrawler"
)

// 根据输入关键字获取图片id
func KeywordStrategy(p *pixivic.Pixivic) {
	keyword := p.KeyWord
	baseGroup, _ := url.QueryUnescape(keyword)
	for i := 1; i <= 50; i++ {
		resp, err := http.Get("https://api.pixivic.com/illustrations?" +
			"illustType=illust&searchType=original&maxSanityLevel=9&page=" +
			strconv.Itoa(i) + "&keyword=" + keyword + "&pageSize=30")
		if err != nil {
			log.Println(err)
		}
		var details = &pixivic.UrlDetail{}
		json.NewDecoder(resp.Body).Decode(details)
		if len(details.Data) < 30 {
			break
		}
		for _, detail := range details.Data {
			picDetail, flag := process(p, &detail)
			if flag && atomic.LoadInt32(&p.IsCancel) == 0 {
				picDetail.Group = baseGroup + "/" + picDetail.Group
				p.PicChan <- picDetail
			}
			for _, detail2 := range getRelevanceUrls(strconv.Itoa(detail.Id), 1, 3) {
				picDetail, flag := process(p, &detail2)
				if flag && atomic.LoadInt32(&p.IsCancel) == 0 {
					picDetail.Group = baseGroup + "/" + picDetail.Group
					p.PicChan <- picDetail
				}
			}
		}
	}
}

// 根据输入图片Id爬取相关图片
func PicIdStrategy(p *pixivic.Pixivic) {
	imgId := p.KeyWord
	pageStart := 3
	for i := 1; i <= 10; i++ {
		details := getRelevanceUrls(imgId, pageStart*(i-1)+1, pageStart*i)
		for _, detail := range details {
			picDetail, flag := process(p, &detail)
			if flag && atomic.LoadInt32(&p.IsCancel) == 0 {
				p.PicChan <- picDetail
			}
			for _, detail2 := range getRelevanceUrls(strconv.Itoa(detail.Id), 1, 5) {
				picDetail, flag := process(p, &detail2)
				if flag && atomic.LoadInt32(&p.IsCancel) == 0 {
					p.PicChan <- picDetail
				}
			}
		}
	}
}

// 根据作者ID爬取其所有图片
func AuthorStrategy(p *pixivic.Pixivic) {
	authorId := p.KeyWord
	author := &struct {
		Data struct {
			Name string
		}
	}{}
	// 通过此接口获取作者名，作为文件夹根目录
	resp, _ := http.Get("https://api.pixivic.com/artists/" + authorId)
	json.NewDecoder(resp.Body).Decode(author)
	baseGroup := author.Data.Name

	for i := 1; ; i++ {
		// 根据作者ID获取作者图片的方法
		originUrl := "https://api.pixivic.com/artists/" + authorId +
			"/illusts/illust?page=" + strconv.Itoa(i) + "&pageSize=30&maxSanityLevel=4"

		resp, err := http.Get(originUrl)
		if err != nil {
			log.Println(err)
			break
		}
		// 存储原始图片信息
		var details = &pixivic.UrlDetail{}
		json.NewDecoder(resp.Body).Decode(details)
		resp.Body.Close()
		for _, detail := range details.Data {
			// 解析图片信息
			picDetail, flag := process(p, &detail)
			// 如果图片信息经解析符合要求且爬虫未停止则向通道添加新的任务
			if flag && atomic.LoadInt32(&p.IsCancel) == 0 {
				picDetail.Group = baseGroup + "/" + picDetail.Group
				p.PicChan <- picDetail
			}
		}
		// 如果当前页不足30个这说明爬取完成了，则退出
		if len(details.Data) < 30 {
			break
		}
	}
}

// 获取图片Id的相关图片
func getRelevanceUrls(imgId string, pageStart, pageEnd int) []pixivic.OriginalDetail {
	var res []pixivic.OriginalDetail
	for i := pageStart; i <= pageEnd; i++ {
		originUrl := "https://api.pixivic.com/illusts/" +
			imgId + "/related?page=" + strconv.Itoa(i) + "&pageSize=30"

		resp, err := http.Get(originUrl)
		if err != nil {
			log.Println(err)
			break
		}
		var details = &pixivic.UrlDetail{}
		json.NewDecoder(resp.Body).Decode(details)
		resp.Body.Close()
		res = append(res, details.Data...)
		if len(details.Data) < 30 {
			break
		}
	}
	return res
}

// 根据图片原始信息加工成要爬取的图片信息
func process(p *pixivic.Pixivic, detail *pixivic.OriginalDetail) (*pixivic.PicDetail, bool) {
	if detail.TotalBookmarks >= p.Bookmarks {
		pic := &pixivic.PicDetail{
			Id:  strconv.Itoa(detail.Id),
			Url: detail.ImageUrls[0].Original,
		}
		flag := false
		h := detail.Height
		w := detail.Width
		isWidth := w > h
		var max, min int
		if w > h {
			max, min = w, h
		} else {
			min, max = w, h
		}
		ratio := float32(max) / float32(min)
		pic.Ratio = ratio
		if max >= 1750 && min >= 950 {
			if ratio < 2.15 && ratio > 1.4 {
				if isWidth {
					if strings.Contains(p.PicType, "w") {
						pic.Group = "宽屏"
						flag = true
					}
				} else {
					if strings.Contains(p.PicType, "h") {
						pic.Group = "竖屏"
						flag = true
					}
				}
			} else {
				if strings.Contains(p.PicType, "o") {
					pic.Group = "其他"
					flag = true
				}
			}
		} else {
			if strings.Contains(p.PicType, "s") {
				pic.Group = "小屏"
				flag = true
			}
		}
		return pic, flag
	}
	return nil, false
}
