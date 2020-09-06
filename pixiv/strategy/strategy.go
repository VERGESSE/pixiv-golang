package strategy

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"pixivic/pixiv"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// 根据输入关键字获取图片id
func KeywordStrategy(p *pixiv.Pixiv) {
	baseGroup := p.KeyWord
	keyword := p.KeyWord +
		"%20" + strconv.Itoa(getMinBookMark(p.Bookmarks)) +
		url.QueryEscape("users入り")
	wltHlt := "&wlt=1000&hlt=1000"
	if strings.Contains(p.PicType, "s") {
		wltHlt = ""
	}
	wait := sync.WaitGroup{}
	for i := 1; ; i++ {
		header := &http.Header{}
		header.Add("user-agent", pixiv.GetRandomUserAgent())
		header.Add("cookie", p.Cookie)
		nowUrl, _ := url.Parse("https://www.pixiv.net/ajax/search/illustrations/" +
			keyword + "?word=" + keyword + "&order=date_d&mode=all" +
			"&p=" + strconv.Itoa(i) + "&s_mode=s_tag&type=illust" + wltHlt)
		request := &http.Request{
			Method: "GET",
			URL:    nowUrl,
			Header: *header,
		}
		resp, err := p.Client.Do(request)
		if err != nil {
			log.Println(err)
			break
		}

		var details = &pixiv.UrlDetail{}
		json.NewDecoder(resp.Body).Decode(details)
		resp.Body.Close()
		for _, detail := range details.Body.Illust.Data {
			picDetail, flag := process(p, &detail, false)
			if flag && atomic.LoadInt32(&p.IsCancel) == 0 {
				picDetail.Group = baseGroup + "/" + picDetail.Group
				p.PicChan <- picDetail
				go func(id string) {
					wait.Add(1)
					for _, detail2 := range getRelevanceUrls(p, id, 50) {
						picDetail, flag := process(p, &detail2, true)
						if flag && atomic.LoadInt32(&p.IsCancel) == 0 {
							picDetail.Group = baseGroup + "/" + picDetail.Group
							p.PicChan <- picDetail
						}
					}
					wait.Done()
				}(detail.Id)
			}
		}
		if len(details.Body.Illust.Data) < 60 {
			log.Println("关键字爬取搜索完成！")
			break
		}
	}
	wait.Wait()
}

// 根据输入图片Id爬取相关图片
func PicIdStrategy(p *pixiv.Pixiv) {
	wait := sync.WaitGroup{}
	imgId := p.KeyWord
	for _, detail := range getRelevanceUrls(p, imgId, 100) {
		picDetail, flag := process(p, &detail, true)
		if flag && atomic.LoadInt32(&p.IsCancel) == 0 {
			p.PicChan <- picDetail
		}
		wait.Add(1)
		go func(id string) {
			for _, detail2 := range getRelevanceUrls(p, id, 100) {
				picDetail, flag := process(p, &detail2, true)
				if flag && atomic.LoadInt32(&p.IsCancel) == 0 {
					p.PicChan <- picDetail
				}
				for _, detail3 := range getRelevanceUrls(p, id, 50) {
					picDetail, flag := process(p, &detail3, true)
					if flag && atomic.LoadInt32(&p.IsCancel) == 0 {
						p.PicChan <- picDetail
					}
				}
			}
			wait.Done()
		}(detail.Id)
	}
	wait.Wait()
}

// 根据作者ID爬取其所有图片
func AuthorStrategy(p *pixiv.Pixiv) {
	log.Println("暂不支持")
}

// 获取图片Id的相关图片
func getRelevanceUrls(p *pixiv.Pixiv, imgId string, limit int) []pixiv.Illust {
	var res []pixiv.Illust
	originUrl := "https://www.pixiv.net/ajax/illust/" + imgId +
		"/recommend/init?limit=" + strconv.Itoa(limit)

	header := &http.Header{}
	header.Add("user-agent", pixiv.GetRandomUserAgent())
	header.Add("cookie", p.Cookie)
	nowUrl, _ := url.Parse(originUrl)
	request := &http.Request{
		Method: "GET",
		URL:    nowUrl,
		Header: *header,
	}
	resp, err := p.Client.Do(request)
	if err != nil {
		return nil
	}
	var details = &pixiv.UrlDetail2{}
	json.NewDecoder(resp.Body).Decode(details)
	resp.Body.Close()
	res = append(res, details.Body.Illusts...)
	return res
}

// 根据图片原始信息加工成要爬取的图片信息
func process(p *pixiv.Pixiv, detail *pixiv.Illust, bookMark bool) (*pixiv.PicDetail, bool) {
	// 如果需要计算点赞数则进行计算
	if bookMark {
		resp, err := p.Client.Get("https://www.pixiv.net/artworks/" + detail.Id)
		if err != nil {
			return nil, false
		}
		bytes, _ := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		respStr := fmt.Sprintf("%s", bytes)
		split := strings.Split(respStr, "bookmarkCount\":")
		if len(split) < 2 {
			return nil, false
		}
		detail.BookmarkData, _ = strconv.Atoi(
			strings.Split(split[1], ",")[0])
		if err != nil {
			return nil, false
		}
	}

	if !bookMark || detail.BookmarkData >= p.Bookmarks {
		pic := &pixiv.PicDetail{
			Id:  detail.Id,
			Url: detail.Url,
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
		if max >= 1900 && min >= 1000 {
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

func getMinBookMark(bookmark int) int {
	for _, num := range pixiv.Bookmark {
		if num >= bookmark {
			return num
		}
	}
	return pixiv.Bookmark[len(pixiv.Bookmark)-1]
}
