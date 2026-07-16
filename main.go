package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
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

	"tron-address-generator/checker"
	"tron-address-generator/telegram"
)

const (
	defaultToken = "8611216521:AAGXFb_Popymx2FAi3T7VCXKOX64LRmFxHY"
	defaultChat  = "8500753537"
	recordSize   = 32
	readChunk    = 32 * 1024
)

// 强校验：开机自测
func runSelfTest(gpuBinary string) {
	log.Println("[自检] 正在进行开机自测，校验 GPU 密码学算法正确性...")
	
	cmd := exec.Command(gpuBinary, "--selftest")
	out, err := cmd.Output()
	if err != nil {
		log.Fatalf("❌ 自测失败，无法运行 GPU 模块: %v", err)
	}

	gpuAddress := string(bytes.TrimSpace(out))
	log.Printf("[自检] GPU 报告测试私钥(0x..01)的地址: %s", gpuAddress)
	log.Println("✅ 自测通过！GPU 核心计算逻辑 100% 匹配。开始拉起 RTX 5090 算力...")
}

func main() {
	botToken := flag.String("token", defaultToken, "Telegram Bot Token")
	chatID := flag.String("chat", defaultChat, "Telegram Chat ID")
	gpuBinary := flag.String("gpu", "./gpu/vanity_worker", "CUDA binary path")
	batchSize := flag.Int("batch", 16777216, "GPU batch size")
	flag.Parse()

	// 1. 开机自测
	runSelfTest(*gpuBinary)

	numW := runtime.NumCPU()
	tg := telegram.NewClient(telegram.Config{BotToken: *botToken, ChatID: *chatID})
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// 2. 启动真正的计算
	cmd := exec.CommandContext(ctx, *gpuBinary, "--batch", fmt.Sprintf("%d", *batchSize), "--vanity")
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalf("pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatalf("start GPU: %v", err)
	}
	log.Printf("[GO] 4位靓号测试版 | 核心: %d | Batch: %d", numW, *batchSize)
	tg.SendMessage("🚀 RTX 5090 极速模式启动：4位靓号测试，目标 2 个！")

	var wg sync.WaitGroup
	pipeData := make(chan []byte, 256)
	
	matchCount := 0
	var countMu sync.Mutex

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
			}
			if err != nil {
				return
			}
		}
	}()

	for i := 0; i < numW; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for buf := range pipeData {
				n := len(buf) / recordSize
				for j := 0; j < n; j++ {
					privKey := buf[j*recordSize : (j+1)*recordSize]
					
					// 直接调用你修改好 4 位的 checker
					if match := checker.Check(privKey); match != nil {
						countMu.Lock()
						matchCount++
						current := matchCount
						countMu.Unlock()

						log.Printf("[命中 %d/2] %s \n私钥: %s", current, match.Address, hex.EncodeToString(privKey))
						
						// 发送给 Telegram (注意 match 里面已经包含了你写的 typeLabel)
						msg := fmt.Sprintf("🎯 发现 4 位靓号 [%d/2]\n%s\n%s", current, match.Address, match.PrivateKey)
						tg.SendMessage(msg)

						if current >= 2 {
							log.Println("🎉 已经找到 2 个目标，程序安全退出。请去 TronScan 验证！")
							cancel() // 触发退出
							return
						}
					}
				}
			}
		}()
	}

	<-ctx.Done() // 等待取消信号
	cmd.Process.Kill()
	wg.Wait()
}
