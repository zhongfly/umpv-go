package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// isURL 检查给定的文件名是否是URL
func isURL(filename string) bool {
	parts := strings.SplitN(filename, "://", 2)
	if len(parts) < 2 {
		return false
	}
	// 协议前缀只允许包含字母、数字和下划线
	prefix := parts[0]
	for _, c := range prefix {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}

// getSocketPath 返回socket或命名管道的路径
func getSocketPath() (string, error) {
	return `\\.\pipe\umpv`, nil
}

// sendFilesToMPV 发送文件列表到MPV
func sendFilesToMPV(conn interface{}, files []string, loadFileFlag string) error {
	for _, f := range files {
		// 转义特殊字符
		f = strings.ReplaceAll(f, "\\", "\\\\")
		f = strings.ReplaceAll(f, "\"", "\\\"")
		f = strings.ReplaceAll(f, "\n", "\\n")
		cmd := fmt.Sprintf("raw loadfile \"%s\" %s\n", f, loadFileFlag)

		switch c := conn.(type) {
		case net.Conn:
			_, err := c.Write([]byte(cmd))
			if err != nil {
				return err
			}
		case *os.File:
			_, err := c.Write([]byte(cmd))
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// startMPV 启动新的MPV进程
func startMPV(files []string, socketPath string) error {
	mpv := "mpv.exe"

	// 检查当前目录中是否存在mpv
	if _, err := os.Stat(mpv); err == nil {
		absPath, err := filepath.Abs(mpv)
		if err == nil {
			mpv = absPath
		}
	}

	// 检查环境变量中是否指定了自定义的MPV命令
	if envMPV := os.Getenv("MPV"); envMPV != "" {
		mpv = envMPV
	}

	args := []string{
		"--input-ipc-server=" + socketPath,
		"--",
	}
	args = append(args, files...)

	cmd := exec.Command(mpv, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

func main() {
	// 定义命令行参数
	var ipcServer string
	flag.StringVar(&ipcServer, "ipc-server", "", "Specify the IPC server socket path")
	var loadFileFlag string
	flag.StringVar(&loadFileFlag, "loadfile-flag", "append-play", "Specify the loadfile flag (replace, append, append-play)")

	// 添加对 --help 参数的处理
	help := flag.Bool("help", false, "Show help message")

	flag.Parse()

	if *help {
		flag.Usage()
		return
	}

	if len(flag.Args()) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s [--ipc-server <path>] [--loadfile-flag <flag>] <files...>\n", os.Args[0])
		os.Exit(1)
	}

	// 处理文件路径
	var files []string
	for _, f := range flag.Args() {
		if isURL(f) {
			files = append(files, f)
		} else {
			absPath, err := filepath.Abs(f)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error resolving path %s: %v\n", f, err)
				continue
			}
			files = append(files, absPath)
		}
	}

	var socketPath string
	var err error
	if ipcServer != "" {
		socketPath = ipcServer
	} else {
		socketPath, err = getSocketPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	// Windows 使用命名管道
	file, err := os.OpenFile(socketPath, os.O_RDWR, 0)
	if err != nil {
		// 如果管道不存在，启动新的MPV实例
		err = startMPV(files, socketPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error starting mpv: %v\n", err)
			os.Exit(1)
		}
		return
	}
	defer file.Close()

	err = sendFilesToMPV(file, files, loadFileFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error sending files to mpv: %v\n", err)
		os.Exit(1)
	}

}
