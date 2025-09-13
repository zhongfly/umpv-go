package main

//go:generate go-winres make --arch=amd64,386,arm64 --file-version=git-tag --product-version=git-tag

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
	"github.com/go-ini/ini"
)

// 常量定义
const (
	DefaultSocketPath   = `\\.\pipe\umpv`
	DefaultLoadFileFlag = "append-play"
	DefaultConfigFile   = "umpv.conf"
	MPVExecutable       = "mpv.exe"
	PipePrefix          = `\\.\pipe\`

	// Windows API 常量
	SW_RESTORE = 9
	BufferSize = 4096
)

// isURL 检查给定的文件名是否是URL
func isURL(filename string) bool {
	index := strings.Index(filename, "://")
	if index == -1 {
		return false
	}
	// 协议前缀只允许包含字母、数字和下划线
	prefix := filename[:index]
	for _, c := range prefix {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}

func addQuotesToStrings(inputs []string) []string {
	result := make([]string, 0, len(inputs))
	for _, str := range inputs {
		str = "\"" + strings.Trim(str, "\"'") + "\""
		result = append(result, str)
	}
	return result
}

// startMPV 启动新的MPV进程
func startMPV(files []string, socketPath string) error {
	mpv := MPVExecutable

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

	var cmdBuilder strings.Builder
	cmdBuilder.WriteString("\"")
	cmdBuilder.WriteString(mpv)
	cmdBuilder.WriteString("\" --input-ipc-server=")
	cmdBuilder.WriteString(socketPath)
	cmdBuilder.WriteString(" --force-window=yes --idle=yes --")

	quotedFiles := addQuotesToStrings(files)
	for _, file := range quotedFiles {
		cmdBuilder.WriteString(" ")
		cmdBuilder.WriteString(file)
	}

	cmd := exec.Command("C:\\Windows\\system32\\cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
		CmdLine:    fmt.Sprintf(`/s /c "%s"`, cmdBuilder.String()),
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}

// 创建用于转义特殊字符的替换器
var escapeReplacer = strings.NewReplacer(
	"\\", "\\\\",
	"\"", "\\\"",
	"\n", "\\n",
)

// sendFilesToMPV 发送文件列表到MPV
func sendFilesToMPV(conn net.Conn, files []string, loadFileFlag string) error {
	for _, f := range files {
		// 转义特殊字符
		f = escapeReplacer.Replace(f)
		cmd := fmt.Sprintf("raw loadfile \"%s\" %s\n", f, loadFileFlag)
		if loadFileFlag == "replace" {
			loadFileFlag = "append"
		}

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

	buf := make([]byte, BufferSize)
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

	procShowWindow.Call(hwnd, SW_RESTORE)
	procSetForegroundWindow.Call(hwnd)

	return nil
}

// Config 结构体用于存储配置信息
type Config struct {
	IpcServer    string `ini:"ipc-server"`
	LoadFileFlag string `ini:"loadfile-flag"`
}

// loadConfig 读取配置文件
func loadConfig(configPath string) (*Config, error) {
	cfg := new(Config)
	err := ini.MapTo(cfg, configPath)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// processSocketPath 处理并验证socket路径
func processSocketPath(ipcServer string) (string, error) {
	if ipcServer == "" {
		return DefaultSocketPath, nil
	}

	ipcServer = strings.ToLower(ipcServer)
	if !strings.HasPrefix(ipcServer, PipePrefix) {
		if strings.Contains(ipcServer, `\`) {
			return "", fmt.Errorf("IPC server socket path is not valid: %v", ipcServer)
		}
		ipcServer = PipePrefix + ipcServer
	}
	return ipcServer, nil
}
func resolveConfig(executableDir, ipcServer, loadFileFlag, configPath string) (string, string, error) {
	// 如果没有指定配置文件路径，则使用默认路径
	if configPath == "" {
		configPath = filepath.Join(executableDir, DefaultConfigFile)
	}

	cfg, err := loadConfig(configPath)
	if err != nil && !os.IsNotExist(err) {
		return "", "", fmt.Errorf("error loading config file: %v", err)
	}

	// 命令行参数覆盖配置文件
	finalIpcServer := ipcServer
	if finalIpcServer == "" && cfg != nil {
		finalIpcServer = cfg.IpcServer
	}

	finalLoadFileFlag := loadFileFlag
	if finalLoadFileFlag == "" {
		if cfg != nil && cfg.LoadFileFlag != "" {
			finalLoadFileFlag = cfg.LoadFileFlag
		} else {
			finalLoadFileFlag = DefaultLoadFileFlag
		}
	}

	return finalIpcServer, finalLoadFileFlag, nil
}

func main() {
	// 定义命令行参数
	var (
		ipcServer    string
		loadFileFlag string
		configPath   string
	)

	flag.StringVar(&ipcServer, "ipc-server", "", "Specify the IPC server socket path")
	flag.StringVar(&loadFileFlag, "loadfile-flag", "", "Specify the loadfile flag (replace, append, append-play)")
	flag.StringVar(&configPath, "config", "", "Specify the config file path")

	// 添加对 --help 参数的处理
	help := flag.Bool("help", false, "Show help message")

	flag.Parse()

	if *help {
		flag.Usage()
		return
	}

	// 切换工作路径为程序所在的文件夹
	executablePath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting executable path: %v", err)
	}
	executableDir := filepath.Dir(executablePath)
	err = os.Chdir(executableDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error changing directory: %v", err)
	}

	// 处理文件路径
	files := make([]string, 0, len(flag.Args()))
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

	// 解析配置
	finalIpcServer, finalLoadFileFlag, err := resolveConfig(executableDir, ipcServer, loadFileFlag, configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	// 处理socket路径
	socketPath, err := processSocketPath(finalIpcServer)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
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

	err = sendFilesToMPV(conn, files, finalLoadFileFlag)
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
