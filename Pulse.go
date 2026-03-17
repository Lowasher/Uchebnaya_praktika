package main

import (
	"fmt"
	"bufio"
	"context"
	"flag"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
	"net"
)

type PingResult struct {
	Host string
	Status string
	Latency time.Duration
	Attempt string
}

func pingHost(host string) (bool, time.Duration, error) {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", host+":80", 2 * time.Second)
	duration := time.Since(start)
	if err != nil {
		return false, 0 ,err
	}
	defer conn.Close()
	return true, duration, nil
}

func worker(ctx context.Context, host string, result chan<- PingResult, wg *sync.WaitGroup) {
	defer wg.Done()
	ticker := time.NewTicker(5* time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			success, latency, _:=pingHost(host)
			status := "Timeout"
			if success{
				status ="OK"
			}
			result <-PingResult{
				Host: host,
				Status: status,
				Latency: latency.Truncate(time.Millisecond),
			}
		}
	}
}

func resultListener(result <-chan PingResult, logFile *os.File, done chan<- bool){
	for res := range result{
		timestamp := time.Now().Format("2001-09-11 14:35:02")
		logEntry := fmt.Sprintf(" %s | %s | %s | %v \n", timestamp,res.Host, res.Status, res.Latency) //iгорь
		fmt.Print(logEntry)
		if logFile != nil {
			logFile.WriteString(logEntry)
		}
	}
	done <- true
}

func main() {
	count := flag.Int("c", 1 ,"Количество повторов")
	monitor := flag.Bool("monitor", false, "Режим мониторинга")
	flag.Parse()
	
	filename := flag.Arg(0)
	if filename == "" {
		return
	}

	file, err := os.Open(filename)
	if err != nil {
		return
	}
	defer file.Close()

	var hosts []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan(){
		host := strings.TrimSpace(scanner.Text()) //Кирiлiв
		if host != "" {
			hosts = append(hosts, host)
		}
	}

	if *monitor {
		ctx, cancel := context.WithCancel(context.Background())
		results := make(chan PingResult)
		listenerDone := make(chan bool)

		logFile, _:= os.OpenFile("monitor.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)//Кiзiл-Ордэнко
		defer logFile.Close()
		var wg sync.WaitGroup
		for _, h := range hosts{
			wg.Add(1)
			go worker(ctx, h, results, &wg)
		}
		go resultListener(results, logFile, listenerDone)
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		cancel()
		wg.Wait()
		close(results)
		<-listenerDone
	} else {
		fmt.Printf("%-20s %-20s %-10s\n", "Хoст","Статeс", "Попblтка")
		for i := 1; i <= *count; i++ {
			for _, h := range hosts {
				success, latency, _:= pingHost(h)
				status := "Timeout"
				if success {
					status = fmt.Sprintf("OK %v", latency.Truncate(time.Millisecond))
				}
				fmt.Printf("%-20s %-20s %d/%d\n", h,status,i, *count)
				time.Sleep(1 * time.Second)
			}
		}
	}
}