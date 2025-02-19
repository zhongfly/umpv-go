package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/Microsoft/go-winio"
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

// sendFilesToMPV 发送文件列表到MPV
func sendFilesToMPV(conn net.Conn, files []string, loadFileFlag string) error {
	for _, f := range files {
		// 转义特殊字符
		f = strings.ReplaceAll(f, "\\", "\\\\")
		f = strings.ReplaceAll(f, "\"", "\\\"")
		f = strings.ReplaceAll(f, "\n", "\\n")
		cmd := fmt.Sprintf("raw loadfile \"%s\" %s\n", f, loadFileFlag)

		_, err := io.WriteString(conn, cmd)
		if err != nil {
			return err
		}
	}
	return nil
}

// Response represents the structure of the JSON response from MPV
type Response struct {
	Data      int    `json:"data"`
	RequestID int    `json:"request_id"`
	Error     string `json:"error"`
}

// getPid 获取MPV进程的PID
func getPid(conn net.Conn) (int, error) {
	cmd := `{"command": ["get_property", "pid"]}` + "\n"
	_, err := io.WriteString(conn, cmd)
	if err != nil {
		return 0, err
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return 0, err
	}

	var response Response
	err = json.Unmarshal(buf[:n], &response)
	if err != nil {
		return 0, err
	}

	if response.Error != "success" {
		return 0, fmt.Errorf("error from MPV: %s", response.Error)
	}

	return response.Data, nil
}

// setForegroundWindow 将指定PID的窗口获得输入焦点
func setForegroundWindow(pid int) error {
	user32 := syscall.NewLazyDLL("user32.dll")
	procSetForegroundWindow := user32.NewProc("SetForegroundWindow")
	procShowWindow := user32.NewProc("ShowWindow")
	procGetWindowThreadProcessId := user32.NewProc("GetWindowThreadProcessId")
	procGetClassName := user32.NewProc("GetClassNameW")
	enumWindows := user32.NewProc("EnumWindows")

	hwnd := uintptr(0)
	cb := func(hwndFound syscall.Handle, lparam uintptr) uintptr {
		var processID uint32
		procGetWindowThreadProcessId.Call(uintptr(hwndFound), uintptr(unsafe.Pointer(&processID)))
		if processID == uint32(pid) {
			className := make([]uint16, 256)
			procGetClassName.Call(uintptr(hwndFound), uintptr(unsafe.Pointer(&className[0])), uintptr(len(className)))
			if syscall.UTF16ToString(className) == "mpv" {
				hwnd = uintptr(hwndFound)
				return 0 // stop enumeration
			}
		}
		return 1 // continue enumeration
	}

	enumWindows.Call(syscall.NewCallback(cb), 0)

	if hwnd == 0 {
		return fmt.Errorf("window not found for PID %d", pid)
	}

	const SW_RESTORE = 9
	procShowWindow.Call(hwnd, SW_RESTORE)
	procSetForegroundWindow.Call(hwnd)

	return nil
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
		socketPath = `\\.\pipe\umpv`
	}

	// Windows 使用命名管道
	conn, err := winio.DialPipe(socketPath, nil)
	if err != nil {
		if os.IsNotExist(err) {
			// 如果管道不存在，启动新的MPV实例
			err = startMPV(files, socketPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error starting mpv: %v\n", err)
				os.Exit(1)
			}
			return
		}
		fmt.Fprintf(os.Stderr, "Error connecting to pipe: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	err = sendFilesToMPV(conn, files, loadFileFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error sending files to mpv: %v\n", err)
		os.Exit(1)
	}

	pid, err := getPid(conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting PID: %v\n", err)
		os.Exit(1)
	}
	err = setForegroundWindow(pid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error setting foreground window: %v\n", err)
		os.Exit(1)
	}

}
