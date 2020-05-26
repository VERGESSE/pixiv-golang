/*
	转移所有图片到一个统一文件夹的工具
*/
package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"sync"
)

var (
	// 控制程序结束栅栏
	waitGroup = sync.WaitGroup{}
	// 文件转移线程池
	fileChan = make(chan struct{}, 20)
)

func main() {

	handTask("H:/wallpaper/P站壁纸/电脑壁纸，质量保证", "images/横屏")
	handTask("H:/wallpaper/P站壁纸/电脑壁纸，质量保证", "images/竖屏")
	handTask("H:/wallpaper/P站壁纸/电脑壁纸，质量保证", "images/长图-方图")
	handTask("H:/wallpaper/P站壁纸/精品小图，手机可用", "images/小图")

	// 等待转移结束
	waitGroup.Wait()
}

//dstDir 目标地址
//srcDir 源地址
func handTask(dstDir, srcDir string) {
	file, _ := os.Open(srcDir)
	infos, _ := file.Readdir(0)
	for _, info := range infos {
		waitGroup.Add(1)
		go transferFile(path.Join(dstDir, info.Name()),
			path.Join(srcDir, info.Name()))
	}
}

// 转移图片逻辑
func transferFile(dst, src string) {
	fileChan <- struct{}{}

	dstFile, _ := os.OpenFile(dst, os.O_RDWR|os.O_CREATE, 0644)
	srcFile, _ := os.Open(src)

	_, err := io.Copy(dstFile, srcFile)
	if err != nil {
		log.Println(err)
	}
	srcFile.Close()
	dstFile.Close()
	// 删除原图片
	os.Remove(src)

	waitGroup.Done()
	<-fileChan
	fmt.Println(src, " 转移完成！")
}
