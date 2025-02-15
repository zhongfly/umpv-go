package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	if runtime.GOOS == "windows" {
		return `\\.\pipe\umpv`, nil
	}

	baseDir := os.Getenv("UMPV_SOCKET_DIR")
	if baseDir == "" {
		baseDir = os.Getenv("XDG_RUNTIME_DIR")
	}
	if baseDir == "" {
		baseDir = os.Getenv("HOME")
	}
	if baseDir == "" {
		baseDir = os.Getenv("TMPDIR")
	}

	if baseDir == "" {
		return "", fmt.Errorf("could not determine socket directory: ensure UMPV_SOCKET_DIR, XDG_RUNTIME_DIR, HOME or TMPDIR is set")
	}

	return filepath.Join(baseDir, ".umpv"), nil
}

// sendFilesToMPV 发送文件列表到MPV
func sendFilesToMPV(conn interface{}, files []string) error {
	for _, f := range files {
		// 转义特殊字符
		f = strings.ReplaceAll(f, "\\", "\\\\")
		f = strings.ReplaceAll(f, "\"", "\\\"")
		f = strings.ReplaceAll(f, "\n", "\\n")
		cmd := fmt.Sprintf("raw loadfile \"%s\" append-play\n", f)

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
	mpv := "mpv"
	if runtime.GOOS == "windows" {
		mpv = "mpv.exe"
	}

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
		"--profile=builtin-pseudo-gui",
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
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <files...>\n", os.Args[0])
		os.Exit(1)
	}

	// 处理文件路径
	var files []string
	for _, f := range os.Args[1:] {
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

	socketPath, err := getSocketPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if runtime.GOOS == "windows" {
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

		err = sendFilesToMPV(file, files)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error sending files to mpv: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Unix 使用 Unix Domain Socket
		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			// 如果socket连接失败，启动新的MPV实例
			err = startMPV(files, socketPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error starting mpv: %v\n", err)
				os.Exit(1)
			}
			return
		}
		defer conn.Close()

		err = sendFilesToMPV(conn, files)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error sending files to mpv: %v\n", err)
			os.Exit(1)
		}
	}
}
