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
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/pflag"

	"github.com/lunatickochiya/rsync-gui/web/internal/server"
)

var version = "dev"

const (
	backgroundEnv      = "RSYNC_WEB_BACKGROUND"
	backgroundTokenEnv = "RSYNC_WEB_TOKEN"
)

const usageText = `rsync-web - 在浏览器里使用 rsync

用法：
  rsync-web [选项]

选项：
  -l, --listen 地址       监听地址（默认 127.0.0.1:8787）
      --token 令牌        访问令牌；非本机监听时未指定会自动生成
      --wsl               WSL 主机访问模式：监听所有网卡并打印 Windows 主机地址
  -b, --background        后台运行，退出终端后继续服务
      --daemon            同 --background
      --pid-file 文件     后台 PID 文件（默认按端口放在配置目录）
      --log-file 文件     后台日志文件（默认按端口放在缓存目录）
      --stop              停止后台实例（配合相同的 --listen / --pid-file）
  -V, --version           显示版本
  -h, --help              显示帮助

示例：
  rsync-web
  rsync-web --background
  rsync-web --stop
  rsync-web --wsl
  rsync-web --listen 0.0.0.0:8787
  rsync-web --listen 127.0.0.1:8787 --token my-secret

远程访问：
  推荐 SSH 隧道：ssh -L 8787:127.0.0.1:8787 user@host
  然后在本机打开 http://127.0.0.1:8787。
  直接监听 0.0.0.0 时会自动生成访问令牌，请使用启动时打印的带令牌链接。
  WSL2 里用 --wsl 会在 Windows 主机上打印可用的 localhost 链接。
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
		wslMode     bool
		background  bool
		stopMode    bool
		pidFile     string
		logFile     string
		showVersion bool
		showHelp    bool
	)
	fs.StringVarP(&listen, "listen", "l", "127.0.0.1:8787", "监听地址")
	fs.StringVar(&token, "token", "", "访问令牌")
	fs.BoolVar(&wslMode, "wsl", false, "WSL 主机访问模式")
	fs.BoolVarP(&background, "background", "b", false, "后台运行")
	fs.BoolVar(&background, "daemon", false, "后台运行（同 --background）")
	fs.BoolVar(&stopMode, "stop", false, "停止后台实例")
	fs.StringVar(&pidFile, "pid-file", "", "后台 PID 文件")
	fs.StringVar(&logFile, "log-file", "", "后台日志文件")
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
	if wslMode {
		if !isWSL() {
			return errors.New("--wsl 只能在 WSL 中运行")
		}
		if isLoopback(host) || host == "" {
			host = "0.0.0.0"
			listen = net.JoinHostPort(host, port)
		}
	}
	wslHost := wslMode || isWSL()
	if pidFile == "" {
		pidFile = defaultPIDFile(port)
	}
	if logFile == "" {
		logFile = defaultLogFile(port)
	}
	if stopMode {
		return stopBackground(pidFile)
	}

	// The background child inherits a generated token through the environment,
	// so the parent can print the access URL before it exits.
	backgroundChild := os.Getenv(backgroundEnv) == "1"
	if token == "" {
		token = os.Getenv(backgroundTokenEnv)
	}
	generated := false
	if token == "" && !isLoopback(host) {
		token, err = randomToken()
		if err != nil {
			return fmt.Errorf("生成访问令牌失败：%w", err)
		}
		generated = true
	}

	if background && !backgroundChild {
		return startBackground(os.Args[1:], listen, host, port, token, generated, wslHost, pidFile, logFile)
	}
	if backgroundChild {
		defer removeOwnPIDFile(pidFile)
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
	for _, line := range accessLines(host, port, token, wslHost) {
		fmt.Println("  " + line)
	}
	if wslHost {
		fmt.Println("\n  Windows 主机优先使用 http://localhost 链接；WSL 网卡地址用于同一虚拟网络。")
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

func startBackground(args []string, listen, host, port, token string, generated, wslHost bool, pidFile, logFile string) error {
	if pid, err := readPIDFile(pidFile); err == nil && processAlive(pid) {
		return fmt.Errorf("后台实例已在运行（PID %d，PID 文件 %s）", pid, pidFile)
	}
	if err := os.MkdirAll(filepath.Dir(pidFile), 0o755); err != nil {
		return fmt.Errorf("无法创建 PID 文件目录：%w", err)
	}
	if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
		return fmt.Errorf("无法创建日志目录：%w", err)
	}
	log, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("无法打开日志文件 %q：%w", logFile, err)
	}
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = withEnv(os.Environ(), backgroundEnv, "1")
	if generated {
		cmd.Env = withEnv(cmd.Env, backgroundTokenEnv, token)
	}
	cmd.Stdin = nil
	cmd.Stdout = log
	cmd.Stderr = log
	detachProcess(cmd)
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		return fmt.Errorf("启动后台进程失败：%w", err)
	}
	pid := cmd.Process.Pid
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		_ = cmd.Process.Kill()
		_ = log.Close()
		return fmt.Errorf("写入 PID 文件 %q 失败：%w", pidFile, err)
	}
	_ = log.Close()

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		_ = os.Remove(pidFile)
		data, _ := os.ReadFile(logFile)
		msg := strings.TrimSpace(string(data))
		if msg != "" {
			msg = "\n" + msg
		}
		if err != nil {
			return fmt.Errorf("后台进程启动失败：%v%s", err, msg)
		}
		return fmt.Errorf("后台进程已退出%s", msg)
	case <-time.After(300 * time.Millisecond):
	}

	fmt.Printf("  ◆ rsync web %s（后台）\n\n", version)
	for _, line := range accessLines(host, port, token, wslHost) {
		fmt.Println("  " + line)
	}
	if wslHost {
		fmt.Println("\n  Windows 主机优先使用 http://localhost 链接；WSL 网卡地址用于同一虚拟网络。")
	}
	if token != "" {
		fmt.Printf("\n  访问令牌：%s\n", token)
		if generated {
			fmt.Println("  （监听在非本机地址，已自动生成令牌）")
		}
	}
	fmt.Printf("\n  PID：%d\n  PID 文件：%s\n  日志：%s\n", pid, pidFile, logFile)
	stopArgs := []string{"--stop", "--listen", listen}
	if pidFile != defaultPIDFile(port) {
		stopArgs = append(stopArgs, "--pid-file", pidFile)
	}
	fmt.Printf("  停止：%s %s\n", filepath.Base(exe), strings.Join(stopArgs, " "))
	return nil
}

func stopBackground(pidFile string) error {
	pid, err := readPIDFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("后台实例未运行（找不到 %s）", pidFile)
		}
		return err
	}
	if !processAlive(pid) {
		_ = os.Remove(pidFile)
		fmt.Printf("后台实例未运行（已清理 %s）\n", pidFile)
		return nil
	}
	if err := terminateProcess(pid); err != nil {
		return fmt.Errorf("停止后台实例 PID %d 失败：%w", pid, err)
	}
	_ = os.Remove(pidFile)
	fmt.Printf("已停止后台实例（PID %d）\n", pid)
	return nil
}

func withEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, item := range env {
		if !strings.HasPrefix(item, prefix) {
			out = append(out, item)
		}
	}
	return append(out, prefix+value)
}

func readPIDFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("PID 文件 %q 无效", path)
	}
	return pid, nil
}

func removeOwnPIDFile(path string) {
	pid, err := readPIDFile(path)
	if err == nil && pid == os.Getpid() {
		_ = os.Remove(path)
	}
}

func defaultPIDFile(port string) string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "rsync-web", "rsync-web-"+port+".pid")
}

func defaultLogFile(port string) string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "rsync-web", "rsync-web-"+port+".log")
}

func accessLines(host, port, token string, wslHost bool) []string {
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
		if wslHost {
			lines = append(lines, "Windows 主机  http://localhost:"+port+path)
		}
		if ip := localIPv4(); ip != "" {
			label := "局域网"
			if wslHost {
				label = "WSL 网卡"
			}
			lines = append(lines, label+"  http://"+ip+":"+port+path)
		}
	default:
		if strings.Contains(displayHost, ":") && !strings.HasPrefix(displayHost, "[") {
			displayHost = "[" + displayHost + "]"
		}
		lines = append(lines, fmt.Sprintf("监听 http://%s:%s", displayHost, port))
		if wslHost {
			lines = append(lines, "Windows 主机  http://localhost:"+port+path)
		}
	}
	if len(lines) == 1 {
		lines[0] += path
	}
	return lines
}

func isWSL() bool {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(data))
	return strings.Contains(lower, "microsoft") || strings.Contains(lower, "wsl")
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
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
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
