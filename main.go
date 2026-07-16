package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"tron-address-generator/checker"
	"tron-address-generator/stats"
	"tron-address-generator/telegram"
)

const (
	defaultToken = "8611216521:AAGXFb_Popymx2FAi3T7VCXKOX64LRmFxHY"
	defaultChat  = "8500753537"
	recordSize   = 32
	readChunk    = 32 * 1024
)

func main() {
	botToken := flag.String("token", defaultToken, "Telegram Bot Token")
	chatID := flag.String("chat", defaultChat, "Telegram Chat ID")
	gpuBinary := flag.String("gpu", "./gpu/vanity_worker", "CUDA binary path")
	
	// 默认 800 万批次，让 CPU 保持 850 K/s 的满载极速状态，不再掉速停顿
	batchSize := flag.Int("batch", 8388608, "GPU batch size") 
	flag.Parse()

	numW := runtime.NumCPU()
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	tg := telegram.NewClient(telegram.Config{BotToken: *botToken, ChatID: *chatID})
	st := stats.NewTracker()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	cmd := exec.CommandContext(ctx, *gpuBinary, "--batch", fmt.Sprintf("%d", *batchSize))
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalf("pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatalf("start GPU: %v", err)
	}
	
	startupMsg := fmt.Sprintf("🚀 RTX 5090 终极严格过滤版启动\n\n🎯 目标: 7位相同 / 尾号6个6 / 尾号6个8\n🖥 核心: %d | 流水线批次: %d\n🔒 引擎: libsecp256k1 权威认证", numW, *batchSize)
	log.Println(startupMsg)
	tg.SendMessage(startupMsg)

	var wg sync.WaitGroup
	pipeData := make(chan []byte, 256)

	// GPU 数据读取协程
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(pipeData)
		br := bufio.NewReaderSize(stdout, 8<<20)
		for {
			buf := make([]byte, readChunk)
			n, err := io.ReadFull(br, buf)
			if n > 0 {
				pipeData <- buf[:n]
				st.AddKeys(uint64(n / recordSize))
			}
			if err != nil {
				return
			}
		}
	}()

	// 状态播报协程 (每 30 分钟发一次 Telegram，每 10 秒打印一次日志)
	wg.Add(1)
	go func() {
		defer wg.Done()
		statTicker := time.NewTicker(10 * time.Second)
		tgTicker := time.NewTicker(30 * time.Minute)
		for {
			select {
			case <-ctx.Done():
				return
			case <-statTicker.C:
				totalKeys, totalMatch, rate, _ := st.Snapshot()
				if totalKeys > 0 {
					log.Printf("[STATS] 扫描: %d | 命中: %d | CPU算力: %s", totalKeys, totalMatch, stats.FormatRate(rate))
				}
			case <-tgTicker.C:
				tg.SendMessage(st.ReportMessage())
			}
		}
	}()

	// 并发验证核心协程
	for i := 0; i < numW; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for buf := range pipeData {
				n := len(buf) / recordSize
				for j := 0; j < n; j++ {
					privKey := buf[j*recordSize : (j+1)*recordSize]

					if match := checker.Check(privKey); match != nil {
						st.AddMatch()
						
						// 这里已经完美匹配了你新版 checker.go 的类型
						typeLabel := map[checker.MatchType]string{
							checker.Suffix7: "后7位相同", 
							checker.Prefix7: "前7位相同", 
							checker.Suffix666666: "尾号6个6", 
							checker.Suffix888888: "尾号6个8",
						}
						
						log.Printf("\n🔥🔥🔥 [大奖命中] %s (%s) \n私钥: %s\n", match.Address, typeLabel[match.Type], match.PrivateKey)
						msg := fmt.Sprintf("%s\n%s\n\n🎯 TRON 靓号 (%s)", match.Address, match.PrivateKey, typeLabel[match.Type])
						tg.SendMessage(msg)
					}
				}
			}
		}()
	}

	<-sigCh
	cancel()
	cmd.Process.Kill()
	wg.Wait()
}
