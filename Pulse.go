package main

import (
    "bufio"
    "context"
    "flag"
    "fmt"
    "net"
    "os"
    "os/signal"
    "strings"
    "sync"
    "syscall"
    "time"

    "golang.org/x/net/icmp"
    "golang.org/x/net/ipv4"
)

type HostReport struct {
    Addr    string
    IsUp    bool
    Elapsed time.Duration
    Msg     string
}

func pingICMP(address string) (bool, time.Duration, error) {
    start := time.Now()
 
    conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
    if err != nil {
        return false, 0, err
    }
    defer conn.Close()

    dest, err := net.ResolveIPAddr("ip", address)
    if err != nil {
        return false, 0, err
    }

    msg := icmp.Message{
        Type: ipv4.ICMPTypeEcho, Code: 0,
        Body: &icmp.Echo{
            ID: os.Getpid() & 0xffff, Seq: 1,
            Data: []byte("PULSE-PING"),
        },
    }
    msgBytes, _ := msg.Marshal(nil)

    if _, err := conn.WriteTo(msgBytes, dest); err != nil {
        return false, 0, err
    }

    reply := make([]byte, 1500)
    err = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
    if err != nil {
        return false, 0, err
    }

    n, _, err := conn.ReadFrom(reply)
    if err != nil {
        return false, 0, err
    }

    _, err = icmp.ParseMessage(1, reply[:n])
    if err != nil {
        return false, 0, err
    }

    return true, time.Since(start), nil
}

func checkNode(address string, useICMP bool) (bool, time.Duration, error) {
    if useICMP {
        return pingICMP(address)
    }
 
    start := time.Now()
    dialer, err := net.DialTimeout("tcp", address+":80", 2*time.Second)
    if err != nil {
        return false, 0, err
    }
    defer dialer.Close()
    return true, time.Since(start), nil
}

func processMonitoring(ctx context.Context, target string, useICMP bool, out chan<- HostReport, group *sync.WaitGroup) {
    defer group.Done()
    heartbeat := time.NewTicker(5 * time.Second)
    defer heartbeat.Stop()

    for {
        select {
            case <-ctx.Done():
                return
            case <-heartbeat.C:
                alive, dur, _ := checkNode(target, useICMP)
                statusTxt := "DOWN"
                if alive { statusTxt = "UP" }
                out <- HostReport{
                Addr: target, IsUp: alive,
                Elapsed: dur.Truncate(time.Millisecond),
                Msg: statusTxt,
            }
        }
    }
}

func main() {
    reps := flag.Int("c", 1, "Количество повторов")
    liveMode := flag.Bool("monitor", false, "Режим мониторинга")
    useICMP := flag.Bool("icmp", false, "Использовать настоящий ICMP ping (требует прав admin/root)")
    flag.Parse()

    filePath := flag.Arg(0)
    if filePath == "" { return }

    rawFile, err := os.Open(filePath)
    if err != nil { return }
    defer rawFile.Close()

    var nodes []string
    scanner := bufio.NewScanner(rawFile)
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        if line != "" { nodes = append(nodes, line) }
    }

    if *liveMode {
        ctx, stop := context.WithCancel(context.Background())
        dataStream := make(chan HostReport)
        var wg sync.WaitGroup
        log, _ := os.OpenFile("monitor.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
        defer log.Close()

        for _, n := range nodes {
            wg.Add(1)
            go processMonitoring(ctx, n, *useICMP, dataStream, &wg)
        }

        interrupt := make(chan os.Signal, 1)
        signal.Notify(interrupt, syscall.SIGINT, syscall.SIGTERM)

        go func() {
            for report := range dataStream {
                now := time.Now().Format("2006-01-02 15:04:05")
                info := fmt.Sprintf("%s | %s | %s | %v\n", now, report.Addr, report.Msg, report.Elapsed)
                fmt.Print(info)
                log.WriteString(info)
            }
        }()

        <-interrupt
        stop()
        wg.Wait()
        close(dataStream)
    } else {
        fmt.Printf("| %-18s | %-12s | %-10s | %-8s |\n", "ADDRESS", "STATUS", "LATENCY", "STEP")
        for i := 1; i <= *reps; i++ {
            for _, n := range nodes {
                ok, timeSpent, _ := checkNode(n, *useICMP)
                state := "TIMEOUT"
                if ok { state = "OK" }
                fmt.Printf("| %-18s | %-12s | %-10v | %d/%d |\n", n, state, timeSpent.Truncate(time.Millisecond), i, *reps)
                time.Sleep(1 * time.Second)
            }
        }
    }
}
