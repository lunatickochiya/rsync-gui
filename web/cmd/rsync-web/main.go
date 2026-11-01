// Command rsync-web serves the rsync-gui browser interface.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/pflag"

	"github.com/lunatickochiya/rsync-gui/web/internal/server"
)

var version = "dev"

const usageText = `rsync-web - 在浏览器里使用 rsync

用法：
  rsync-web [选项]

选项：
  -l, --listen 地址       监听地址（默认 127.0.0.1:8787）
      --token 令牌        访问令牌；非本机监听时未指定会自动生成
  -V, --version           显示版本
  -h, --help              显示帮助

示例：
  rsync-web
  rsync-web --listen 0.0.0.0:8787
  rsync-web --listen 127.0.0.1:8787 --token my-secret

远程访问：
  推荐 SSH 隧道：ssh -L 8787:127.0.0.1:8787 user@host
  然后在本机打开 http://127.0.0.1:8787。
  直接监听 0.0.0.0 时会自动生成访问令牌，请使用启动时打印的带令牌链接。
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "rsync-web: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := pflag.NewFlagSet("rsync-web", pflag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.SortFlags = false

	var (
		listen      string
		token       string
		showVersion bool
		showHelp    bool
	)
	fs.StringVarP(&listen, "listen", "l", "127.0.0.1:8787", "监听地址")
	fs.StringVar(&token, "token", "", "访问令牌")
	fs.BoolVarP(&showVersion, "version", "V", false, "显示版本")
	fs.BoolVarP(&showHelp, "help", "h", false, "显示帮助")
	fs.Usage = func() { fmt.Fprint(os.Stderr, usageText) }

	if err := fs.Parse(args); err != nil {
		return err
	}
	if showHelp {
		fs.Usage()
		return nil
	}
	if showVersion {
		fmt.Printf("rsync-web %s\n", version)
		return nil
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("未知参数：%s", strings.Join(fs.Args(), " "))
	}

	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("监听地址 %q 无效：%w", listen, err)
	}
	generated := false
	if token == "" && !isLoopback(host) {
		token, err = randomToken()
		if err != nil {
			return fmt.Errorf("生成访问令牌失败：%w", err)
		}
		generated = true
	}

	srv, err := server.New(server.Config{Version: version, Listen: listen, Token: token})
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Addr:              listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	fmt.Printf("  ◆ rsync web  %s\n", version)
	fmt.Printf("  %s\n\n", srv.RsyncVersion())
	for _, line := range accessLines(host, port, token) {
		fmt.Println("  " + line)
	}
	if token != "" {
		fmt.Printf("\n  访问令牌：%s\n", token)
		if generated {
			fmt.Println("  （监听在非本机地址，已自动生成令牌）")
		}
	}
	fmt.Println()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	err = httpServer.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func accessLines(host, port, token string) []string {
	path := "/"
	if token != "" {
		path = "/?token=" + token
	}
	displayHost := host
	var lines []string
	switch {
	case host == "" || host == "0.0.0.0" || host == "::":
		lines = append(lines, fmt.Sprintf("监听 http://0.0.0.0:%s（所有网卡）", port))
		lines = append(lines, "本机   http://127.0.0.1:"+port+path)
		if ip := localIPv4(); ip != "" {
			lines = append(lines, "局域网 http://"+ip+":"+port+path)
		}
	default:
		if strings.Contains(displayHost, ":") && !strings.HasPrefix(displayHost, "[") {
			displayHost = "[" + displayHost + "]"
		}
		lines = append(lines, fmt.Sprintf("监听 http://%s:%s", displayHost, port))
	}
	if len(lines) == 1 {
		lines[0] += path
	}
	return lines
}

func isLoopback(host string) bool {
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func localIPv4() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() {
			continue
		}
		if ip4 := ipNet.IP.To4(); ip4 != nil {
			return ip4.String()
		}
	}
	return ""
}

func randomToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
