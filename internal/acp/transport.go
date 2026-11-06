package acp

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// 连接层的错误。
var (
	// errPeerClosed 表示连接已被本地关闭，后续调用直接失败。
	errPeerClosed = errors.New("acp: connection closed")
	// ErrNoAgent 表示配置里没有这个 agent。
	ErrNoAgent = errors.New("acp: agent not configured")
)

const (
	// MaxMessageBytes 是单条 JSON-RPC 消息的上限（16MB）。
	//
	// 必须有上限：ACP agent 是外部进程，一行无限增长的输出会把宿主内存吃光。
	// 16MB 足以容纳一次带图片的 session/update（图片是 base64，膨胀 4/3）。
	MaxMessageBytes = 16 << 20

	// stderrTailBytes 是保留的 stderr 尾巴长度，用于报错时给出上下文。
	stderrTailBytes = 8 << 10

	// defaultCloseGrace 是关闭时等待子进程自行退出的时间，超时强杀。
	defaultCloseGrace = 5 * time.Second
)

// Transport 是一条 ACP 消息通道：按行收发 JSON-RPC 文本。
type Transport interface {
	// ReadMessage 返回一条完整消息（不含行尾换行）。
	ReadMessage() ([]byte, error)
	// WriteMessage 写出一条消息，实现负责补行尾换行并串行化写入。
	WriteMessage([]byte) error
	// Close 关闭通道并回收资源。
	Close() error
	// Diagnostics 返回对端 stderr 的尾巴，用于错误信息。
	Diagnostics() string
}

// tailBuffer 是固定容量的环形字节缓冲，只保留最后 N 字节。
// stderr 必须**从启动起就持续排空**：管道缓冲区（通常 64KB）写满后子进程
// 会阻塞在 write 上，整条连接随之假死——旧版只在 Close 时才 io.Copy，正是
// 这个死法。
type tailBuffer struct {
	mu   sync.Mutex
	data []byte
	cap  int
}

func newTailBuffer(size int) *tailBuffer {
	return &tailBuffer{cap: size}
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.data = append(t.data, p...)
	if len(t.data) > t.cap {
		trimmed := make([]byte, t.cap)
		copy(trimmed, t.data[len(t.data)-t.cap:])
		t.data = trimmed
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.data)
}

// streamTransport 是建立在任意 io.Reader/Writer 上的按行分帧实现。
// stdio 与测试用的管道都复用它，保证两边的分帧逻辑只有一份。
type streamTransport struct {
	r       *bufio.Reader
	w       io.Writer
	closer  io.Closer
	rCloser io.Closer
	writeMu sync.Mutex
	diag    func() string

	closeOnce sync.Once
	closeErr  error
}

func newStreamTransport(r io.Reader, w io.Writer, closer io.Closer, diag func() string) *streamTransport {
	if diag == nil {
		diag = func() string { return "" }
	}
	t := &streamTransport{
		r:      bufio.NewReaderSize(r, 64<<10),
		w:      w,
		closer: closer,
		diag:   diag,
	}
	// 读端也要能关：Close 之后读取协程若还阻塞在 Read 上，就永远不会退出，
	// 调用方（Client.Close）只能靠超时兜底，测试里每个用例白等 2s。
	if rc, ok := r.(io.Closer); ok {
		t.rCloser = rc
	}
	return t
}

// ReadMessage 读取一行并返回去掉空白后的内容。
//
// 用 ReadSlice 累加而不是 ReadBytes：前者返回 bufio.ErrBufferFull 时可以先
// 检查累计长度，超限立刻报错，不会像 ReadBytes 那样先把整行读进内存再判定。
func (t *streamTransport) ReadMessage() ([]byte, error) {
	for {
		var buf []byte
		for {
			chunk, err := t.r.ReadSlice('\n')
			buf = append(buf, chunk...)
			if len(buf) > MaxMessageBytes {
				return nil, fmt.Errorf("acp: message exceeds %d bytes", MaxMessageBytes)
			}
			if err == nil {
				break
			}
			if errors.Is(err, bufio.ErrBufferFull) {
				continue
			}
			if errors.Is(err, io.EOF) {
				// 容忍最后一行没有换行符：有些实现退出前不补 \n。
				if len(bytes.TrimSpace(buf)) > 0 {
					break
				}
				return nil, io.EOF
			}
			return nil, err
		}

		line := bytes.TrimSpace(buf)
		if len(line) == 0 {
			continue // 空行（keepalive）跳过
		}
		return line, nil
	}
}

func (t *streamTransport) WriteMessage(data []byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	out := make([]byte, 0, len(data)+1)
	out = append(out, data...)
	out = append(out, '\n')
	_, err := t.w.Write(out)
	return err
}

func (t *streamTransport) Close() error {
	t.closeOnce.Do(func() {
		if t.closer != nil {
			t.closeErr = t.closer.Close()
		}
		if t.rCloser != nil {
			_ = t.rCloser.Close()
		}
	})
	return t.closeErr
}

func (t *streamTransport) Diagnostics() string { return t.diag() }

// StdioOptions 是启动 ACP 子进程的参数。
type StdioOptions struct {
	Command string
	Args    []string
	Env     []string // "KEY=VALUE" 形式，叠加在宿主环境之上
	Dir     string   // 子进程工作目录，空 = 继承
}

// StdioTransport 通过子进程的 stdin/stdout 传输 ACP 消息。
type StdioTransport struct {
	cmd    *exec.Cmd
	inner  *streamTransport
	stderr *tailBuffer
	grace  time.Duration

	closeOnce sync.Once
	closeErr  error
}

// StartStdio 启动 ACP agent 子进程。
func StartStdio(opts StdioOptions) (*StdioTransport, error) {
	if opts.Command == "" {
		return nil, errors.New("acp: empty command")
	}

	cmd := exec.Command(opts.Command, opts.Args...)
	cmd.Env = append(os.Environ(), opts.Env...)
	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}
	setProcessAttrs(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("acp: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("acp: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("acp: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("acp: start %s: %w", opts.Command, err)
	}

	tail := newTailBuffer(stderrTailBytes)
	go func() { _, _ = io.Copy(tail, stderr) }()

	t := &StdioTransport{cmd: cmd, stderr: tail, grace: defaultCloseGrace}
	t.inner = newStreamTransport(stdout, stdin, stdin, tail.String)
	return t, nil
}

func (t *StdioTransport) ReadMessage() ([]byte, error)   { return t.inner.ReadMessage() }
func (t *StdioTransport) WriteMessage(data []byte) error { return t.inner.WriteMessage(data) }
func (t *StdioTransport) Diagnostics() string            { return t.stderr.String() }

// PID 返回子进程 PID（诊断/日志用）。
func (t *StdioTransport) PID() int { return t.cmd.Process.Pid }

// Close 先关 stdin 让子进程自行退出，超时才强杀。
//
// ACP agent 收到 EOF 通常会收尾退出；直接 Kill 会丢掉它正在写的日志，
// 排查问题时等于自断线索。
func (t *StdioTransport) Close() error {
	t.closeOnce.Do(func() {
		if t.inner != nil {
			_ = t.inner.Close()
		}

		done := make(chan error, 1)
		go func() { done <- t.cmd.Wait() }()

		select {
		case <-done:
			// 子进程退出码非零是它自己的事，不当连接错误。
		case <-time.After(t.grace):
			killProcessGroup(t.cmd.Process)
			<-done
			t.closeErr = fmt.Errorf("acp: agent did not exit within %s, killed", t.grace)
		}
	})
	return t.closeErr
}
