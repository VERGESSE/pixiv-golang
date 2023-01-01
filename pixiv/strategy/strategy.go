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
	"time"
)

// 根据输入关键字获取图片id
func KeywordStrategy(p *pixiv.Pixiv) {
	baseGroup, _ := url.QueryUnescape(p.KeyWord)
	keyword := p.KeyWord +
		"%20" + strconv.Itoa(getMinBookMark(p.Bookmarks)) +
		url.QueryEscape("users入り")

	wltHlt := "&wlt=1000&hlt=1000"
	if strings.Contains(p.PicType, "s") {
		wltHlt = ""
	}
	total := 0
	retryTime := 0
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
		resp, err := p.DoRequest(request)
		if err != nil && retryTime < 10 {
			log.Println(err)
			retryTime++
			i--
			time.Sleep(time.Millisecond * 500)
			continue
		}

		var details = &pixiv.UrlDetail{}
		json.NewDecoder(resp.Body).Decode(details)
		if i == 1 {
			total = details.Body.Illust.Total
			log.Println("共 ", total, "张待选, ", total/60, " 页待爬取")
		}
		resp.Body.Close()
		if len(details.Body.Illust.Data) == 0 && retryTime < 10 {
			retryTime++
			log.Println("第 ", i, "页获取0条数据，正在重试"+strconv.Itoa(retryTime)+"...")
			i--
			time.Sleep(time.Millisecond * 500)
			continue
		}
		retryTime = 0
		log.Println("第 ", i, "页待选 ", len(details.Body.Illust.Data), " 张")
		num := 0
		// 每页解析是否爬取并行
		countdown := sync.WaitGroup{}
		for _, detail := range details.Body.Illust.Data {
			// 不爬已经爬过的
			if p.RepetitionOdds == 0 && p.Memo[detail.Id] {
				continue
			}
			// 正在执行任务计数
			countdown.Add(1)
			go func(detail pixiv.Illust) {
				picDetail, flag := process(p, &detail, true)
				if flag && atomic.LoadInt32(&p.IsCancel) == 0 {
					picDetail.Group = baseGroup + "/" + picDetail.Group
					num++
					p.PicChan <- picDetail
				}
				countdown.Done()
			}(detail)
		}
		// 等待任务执行完成
		countdown.Wait()
		log.Println("第 ", i, "页筛选出 ", num, " 张")
		if 60*i > total {
			log.Println("关键字爬取搜索完成！")
			break
		}
		if atomic.LoadInt32(&p.IsCancel) != 0 {
			break
		}
	}
}

// 根据输入关键字获取图片id 新版本
func KeywordStrategy0(p *pixiv.Pixiv) {
	nowTime := *p.EndTime
	baseGroup, _ := url.QueryUnescape(p.KeyWord)
	for nowTime.Year() > 2008 {
		// 时间段
		timeQuantum := nowTime.AddDate(0, -3, 0).Format("2006-01-02") + "至" +
			nowTime.Format("2006-01-02")
		// 获取当前时间段第一页
		firstPage := doRequest(p, 1, 0, &nowTime)
		total := firstPage.Body.Illust.Total
		log.Println(timeQuantum+" 共 ", total, "张待选, ", total/60, " 页待爬取")
		// 每页解析是否爬取并行
		countdown := sync.WaitGroup{}
		for i := 1; i <= total/60; i++ {
			details := doRequest(p, i, 0, &nowTime)
			//num := 0
			for _, detail := range details.Body.Illust.Data {
				// 不爬已经爬过的
				if p.RepetitionOdds == 0 && p.Memo[detail.Id] {
					continue
				}
				// 正在执行任务计数
				countdown.Add(1)
				go func(detail pixiv.Illust) {
					picDetail, flag := process(p, &detail, true)
					if flag && atomic.LoadInt32(&p.IsCancel) == 0 {
						picDetail.Group = baseGroup + "/" + picDetail.Group
						//num++
						p.PicChan <- picDetail
					}
					countdown.Done()
				}(detail)
			}
			//log.Println(timeQuantum+" 第 ", i, "页筛选出 ", num, " 张")
		}
		// 如果主动关闭，则退出
		if atomic.LoadInt32(&p.IsCancel) != 0 {
			break
		}
		// 等待任务执行完成
		countdown.Wait()
		// 当前时间递减三个月
		nowTime = nowTime.AddDate(0, -3, 0)
	}
}

func doRequest(p *pixiv.Pixiv, page int, retryTime int, endTime *time.Time) *pixiv.UrlDetail {
	keyword := p.KeyWord +
		"%20" + strconv.Itoa(getMinBookMark(p.Bookmarks)) +
		url.QueryEscape("users入り")

	wltHlt := "&wlt=1000&hlt=1000"
	if strings.Contains(p.PicType, "s") {
		wltHlt = ""
	}
	header := &http.Header{}
	header.Add("user-agent", pixiv.GetRandomUserAgent())
	header.Add("cookie", p.Cookie)
	urlStr := "https://www.pixiv.net/ajax/search/illustrations/" +
		keyword + "?word=" + keyword + "&order=date_d&mode=all" +
		"&p=" + strconv.Itoa(page) + "&s_mode=s_tag&type=illust" + wltHlt
	if endTime != nil {
		urlStr += "&scd=" + endTime.AddDate(0, -3, 0).Format("2006-01-02") +
			"&ecd=" + endTime.Format("2006-01-02")
	}
	nowUrl, _ := url.Parse(urlStr)
	request := &http.Request{
		Method: "GET",
		URL:    nowUrl,
		Header: *header,
	}
	var details = &pixiv.UrlDetail{}
	resp, err := p.DoRequest(request)
	// 失败重试10次
	if err != nil && retryTime < 10 {
		log.Println(err)
		details = doRequest(p, page, retryTime+1, endTime)
		time.Sleep(time.Millisecond * 500)
	} else {
		json.NewDecoder(resp.Body).Decode(details)
		if details.Body.Illust.Total == 0 {
			details = doRequest(p, page, retryTime+1, endTime)
		}
	}
	resp.Body.Close()
	return details
}

// 根据输入图片Id爬取相关图片
func PicIdStrategy(p *pixiv.Pixiv) {
	wait := sync.WaitGroup{}
	imgIds, _ := url.QueryUnescape(p.KeyWord)
	mutex := p.Mutex
	complete := make(map[string]bool)
	for _, imgId := range strings.Split(imgIds, ",") {
		for _, detail := range getRelevanceUrls(p, imgId, 100, 3) {
			mutex.Lock()
			if !complete[detail.Id] && (p.RepetitionOdds > 0 || !p.Memo[detail.Id]) {
				complete[detail.Id] = true
				mutex.Unlock()
				if atomic.LoadInt32(&p.IsCancel) == 0 {
					picDetail, flag := process(p, &detail, true)
					if flag && atomic.LoadInt32(&p.IsCancel) == 0 {
						p.PicChan <- picDetail
					}
				}
			} else {
				mutex.Unlock()
			}
			wait.Add(1)
			go func(id string) {
				for _, detail2 := range getRelevanceUrls(p, id, 100, 3) {
					mutex.Lock()
					if !complete[detail2.Id] && (p.RepetitionOdds > 0 || !p.Memo[detail2.Id]) {
						complete[detail2.Id] = true
						mutex.Unlock()
						if atomic.LoadInt32(&p.IsCancel) == 0 {
							picDetail, flag := process(p, &detail2, true)
							if flag && atomic.LoadInt32(&p.IsCancel) == 0 {
								p.PicChan <- picDetail
							}
						}
					} else {
						mutex.Unlock()
					}
					for _, detail3 := range getRelevanceUrls(p, id, 50, 3) {
						mutex.Lock()
						if !complete[detail3.Id] && (p.RepetitionOdds > 0 || !p.Memo[detail3.Id]) {
							complete[detail3.Id] = true
							mutex.Unlock()
							if atomic.LoadInt32(&p.IsCancel) == 0 {
								picDetail, flag := process(p, &detail3, true)
								if flag && atomic.LoadInt32(&p.IsCancel) == 0 {
									p.PicChan <- picDetail
								}
							}
						} else {
							mutex.Unlock()
						}
					}
				}
				wait.Done()
			}(detail.Id)
		}
	}
	wait.Wait()
}

// 根据作者ID爬取其所有图片
func AuthorStrategy(p *pixiv.Pixiv) {
	log.Println("暂不支持")
}

// 获取图片Id的相关图片
func getRelevanceUrls(p *pixiv.Pixiv, imgId string, limit int, tryTimes int) []pixiv.Illust {
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
	resp, err := p.DoRequest(request)
	if err != nil {
		if tryTimes > 0 {
			return getRelevanceUrls(p, imgId, limit, tryTimes-1)
		}
		log.Println("相关图片爬取失败", err)
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

	pic := &pixiv.PicDetail{
		Id:    detail.Id,
		Url:   detail.Url,
		Group: "",
	}
	for _, tag := range detail.Tags {
		if strings.Contains(strings.ToLower(tag), "r-18") {
			pic.Group += "R-18/"
			if !p.R18 {
				return nil, false
			}
		}
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
	if max >= 1750 && min >= 900 {
		if ratio < 2.15 && ratio > 1.4 {
			if isWidth {
				if strings.Contains(p.PicType, "w") {
					pic.Group += "宽屏"
					flag = true
				}
			} else {
				if strings.Contains(p.PicType, "h") {
					pic.Group += "竖屏"
					flag = true
				}
			}
		} else {
			if strings.Contains(p.PicType, "o") {
				pic.Group += "其他"
				flag = true
			}
		}
	} else {
		if strings.Contains(p.PicType, "s") {
			pic.Group += "小屏"
			flag = true
		}
	}

	if flag && bookMark {
		// 如果需要计算点赞数则进行计算
		header := &http.Header{}
		header.Add("user-agent", pixiv.GetRandomUserAgent())
		header.Add("cookie", p.Cookie)
		nowUrl, _ := url.Parse("https://www.pixiv.net/artworks/" + detail.Id)
		request := &http.Request{
			Method: "GET",
			URL:    nowUrl,
			Header: *header,
		}
		resp, err := p.DoRequest(request)
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
		// 点赞数不符合
		if detail.BookmarkData < p.Bookmarks {
			return pic, false
		}
	}
	return pic, flag
}

func getMinBookMark(bookmark int) int {
	for index, num := range pixiv.Bookmark {
		if num > bookmark {
			return pixiv.Bookmark[index-1]
		}
	}
	return pixiv.Bookmark[len(pixiv.Bookmark)-1]
}
