package tool

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// EmailConfig 邮件配置
type EmailConfig struct {
	SMTPHost     string `json:"smtp_host" yaml:"smtp_host"`         // SMTP 服务器地址
	SMTPPort     int    `json:"smtp_port" yaml:"smtp_port"`         // SMTP 端口
	Username     string `json:"username" yaml:"username"`           // 用户名
	Password     string `json:"password" yaml:"password"`           // 密码或授权码
	From         string `json:"from" yaml:"from"`                   // 发件人地址
	FromName     string `json:"from_name" yaml:"from_name"`         // 发件人名称
	UseTLS       bool   `json:"use_tls" yaml:"use_tls"`             // 是否使用 TLS
	UseStartTLS  bool   `json:"use_starttls" yaml:"use_starttls"`   // 是否使用 STARTTLS
	InsecureSkip bool   `json:"insecure_skip" yaml:"insecure_skip"` // 跳过 TLS 证书验证
}

// EmailAttachment 邮件附件
type EmailAttachment struct {
	Filename    string `json:"filename"`     // 文件名
	ContentType string `json:"content_type"` // 内容类型
	Data        []byte `json:"data"`         // 文件数据
	Path        string `json:"path"`         // 文件路径（与 Data 二选一）
}

// EmailMessage 邮件消息
type EmailMessage struct {
	To          []string          `json:"to"`          // 收件人
	Cc          []string          `json:"cc"`          // 抄送
	Bcc         []string          `json:"bcc"`         // 密送
	Subject     string            `json:"subject"`     // 主题
	Body        string            `json:"body"`        // 正文
	IsHTML      bool              `json:"is_html"`     // 是否为 HTML
	Attachments []EmailAttachment `json:"attachments"` // 附件
	Headers     map[string]string `json:"headers"`     // 自定义头
}

// EmailTool 邮件发送工具
type EmailTool struct {
	config *EmailConfig
}

// NewEmailTool 创建邮件发送工具
func NewEmailTool(config *EmailConfig) *EmailTool {
	return &EmailTool{config: config}
}

// Name 返回工具名称
func (t *EmailTool) Name() string {
	return "send_email"
}

// Description 返回工具描述
func (t *EmailTool) Description() string {
	return "Send email via SMTP with support for HTML/text format and attachments"
}

// Schema 返回工具参数 Schema
func (t *EmailTool) Schema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"to": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "List of recipient email addresses",
			},
			"cc": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "List of CC email addresses",
			},
			"bcc": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "List of BCC email addresses",
			},
			"subject": map[string]interface{}{
				"type":        "string",
				"description": "Email subject",
			},
			"body": map[string]interface{}{
				"type":        "string",
				"description": "Email body content",
			},
			"is_html": map[string]interface{}{
				"type":        "boolean",
				"description": "Whether body is HTML (default: false)",
			},
			"attachments": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"filename": map[string]interface{}{
							"type":        "string",
							"description": "Attachment filename",
						},
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Path to the file to attach",
						},
						"content_type": map[string]interface{}{
							"type":        "string",
							"description": "MIME content type (optional, auto-detected if not provided)",
						},
					},
					"required": []string{"filename"},
				},
				"description": "List of attachments",
			},
		},
		"required": []string{"to", "subject", "body"},
	}
}

// Execute 执行邮件发送
func (t *EmailTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	if t.config == nil {
		return nil, fmt.Errorf("email configuration not set")
	}

	// 解析参数
	to, err := parseStringSlice(params["to"])
	if err != nil {
		return nil, fmt.Errorf("invalid 'to' parameter: %w", err)
	}
	if len(to) == 0 {
		return nil, fmt.Errorf("at least one recipient is required")
	}

	cc, _ := parseStringSlice(params["cc"])
	bcc, _ := parseStringSlice(params["bcc"])

	subject, ok := params["subject"].(string)
	if !ok || subject == "" {
		return nil, fmt.Errorf("subject is required")
	}

	body, ok := params["body"].(string)
	if !ok {
		return nil, fmt.Errorf("body is required")
	}

	isHTML := false
	if v, ok := params["is_html"].(bool); ok {
		isHTML = v
	}

	// 解析附件
	var attachments []EmailAttachment
	if attData, ok := params["attachments"].([]interface{}); ok {
		for _, att := range attData {
			if attMap, ok := att.(map[string]interface{}); ok {
				attachment := EmailAttachment{}
				if filename, ok := attMap["filename"].(string); ok {
					attachment.Filename = filename
				}
				if path, ok := attMap["path"].(string); ok {
					attachment.Path = path
					// 安全修复（P0）：附件路径必须经过工作目录安全解析
					// （工作目录限制 + 符号链接检查），防止读取任意系统文件作为附件外发
					safePath, err := resolvePath(ctx, path)
					if err != nil {
						return nil, fmt.Errorf("attachment path rejected: %w", err)
					}
					// 读取文件
					data, err := os.ReadFile(safePath)
					if err != nil {
						return nil, fmt.Errorf("failed to read attachment %s: %w", path, err)
					}
					attachment.Data = data
				}
				if contentType, ok := attMap["content_type"].(string); ok {
					attachment.ContentType = contentType
				} else {
					attachment.ContentType = detectContentType(attachment.Filename)
				}
				attachments = append(attachments, attachment)
			}
		}
	}

	// 创建邮件消息
	msg := &EmailMessage{
		To:          to,
		Cc:          cc,
		Bcc:         bcc,
		Subject:     subject,
		Body:        body,
		IsHTML:      isHTML,
		Attachments: attachments,
		Headers:     make(map[string]string),
	}

	// 发送邮件
	if err := t.sendEmail(ctx, msg); err != nil {
		return nil, fmt.Errorf("failed to send email: %w", err)
	}

	return map[string]interface{}{
		"success": true,
		"to":      to,
		"subject": subject,
	}, nil
}

// sendEmail 发送邮件
func (t *EmailTool) sendEmail(ctx context.Context, msg *EmailMessage) error {
	// 构建邮件内容
	emailData, err := t.buildEmail(msg)
	if err != nil {
		return fmt.Errorf("failed to build email: %w", err)
	}

	// 构建收件人列表
	recipients := make([]string, 0, len(msg.To)+len(msg.Cc)+len(msg.Bcc))
	recipients = append(recipients, msg.To...)
	recipients = append(recipients, msg.Cc...)
	recipients = append(recipients, msg.Bcc...)

	// 发送
	addr := fmt.Sprintf("%s:%d", t.config.SMTPHost, t.config.SMTPPort)

	var auth smtp.Auth
	if t.config.Username != "" && t.config.Password != "" {
		auth = smtp.PlainAuth("", t.config.Username, t.config.Password, t.config.SMTPHost)
	}

	// 使用带超时的发送方式
	done := make(chan error, 1)
	go func() {
		done <- smtp.SendMail(addr, auth, t.config.From, recipients, emailData)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// buildEmail 构建邮件内容
func (t *EmailTool) buildEmail(msg *EmailMessage) ([]byte, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// 构建邮件头
	from := t.config.From
	if t.config.FromName != "" {
		from = fmt.Sprintf("%s <%s>", mimeEncode(t.config.FromName), t.config.From)
	}

	headers := make(textproto.MIMEHeader)
	headers.Set("From", from)
	headers.Set("To", strings.Join(msg.To, ", "))
	if len(msg.Cc) > 0 {
		headers.Set("Cc", strings.Join(msg.Cc, ", "))
	}
	headers.Set("Subject", mimeEncode(msg.Subject))
	headers.Set("Date", time.Now().Format(time.RFC1123Z))
	headers.Set("MIME-Version", "1.0")
	headers.Set("Content-Type", fmt.Sprintf("multipart/mixed; boundary=%s", writer.Boundary()))

	// 添加自定义头
	for k, v := range msg.Headers {
		headers.Set(k, v)
	}

	// 写入头
	for k, v := range headers {
		for _, val := range v {
			buf.WriteString(fmt.Sprintf("%s: %s\r\n", k, val))
		}
	}
	buf.WriteString("\r\n")

	// 添加邮件正文部分
	contentType := "text/plain"
	if msg.IsHTML {
		contentType = "text/html"
	}

	bodyPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {fmt.Sprintf("%s; charset=UTF-8", contentType)},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return nil, err
	}

	qpWriter := quotedprintable.NewWriter(bodyPart)
	if _, err := qpWriter.Write([]byte(msg.Body)); err != nil {
		return nil, err
	}
	qpWriter.Close()

	// 添加附件
	for _, att := range msg.Attachments {
		if err := t.addAttachment(writer, att); err != nil {
			return nil, fmt.Errorf("failed to add attachment %s: %w", att.Filename, err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// addAttachment 添加附件
func (t *EmailTool) addAttachment(writer *multipart.Writer, att EmailAttachment) error {
	header := textproto.MIMEHeader{}
	header.Set("Content-Type", att.ContentType)
	header.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, att.Filename))
	header.Set("Content-Transfer-Encoding", "base64")

	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}

	encoder := base64.NewEncoder(base64.StdEncoding, part)
	if _, err := encoder.Write(att.Data); err != nil {
		return err
	}
	encoder.Close()

	return nil
}

// mimeEncode 对字符串进行 MIME 编码（处理非 ASCII 字符）
func mimeEncode(s string) string {
	// 如果字符串只包含 ASCII 字符，直接返回
	isASCII := true
	for _, r := range s {
		if r > 127 {
			isASCII = false
			break
		}
	}
	if isASCII {
		return s
	}

	// 使用 RFC 2047 编码
	return "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(s)) + "?="
}

// detectContentType 检测文件内容类型
func detectContentType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".pdf":
		return "application/pdf"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".txt":
		return "text/plain"
	case ".html", ".htm":
		return "text/html"
	case ".xml":
		return "application/xml"
	case ".json":
		return "application/json"
	case ".zip":
		return "application/zip"
	case ".doc":
		return "application/msword"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xls":
		return "application/vnd.ms-excel"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".ppt":
		return "application/vnd.ms-powerpoint"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	default:
		return "application/octet-stream"
	}
}

// parseStringSlice 解析字符串或字符串切片
func parseStringSlice(v interface{}) ([]string, error) {
	if v == nil {
		return nil, nil
	}

	switch val := v.(type) {
	case string:
		if val == "" {
			return nil, nil
		}
		return []string{val}, nil
	case []string:
		return val, nil
	case []interface{}:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				result = append(result, s)
			} else {
				return nil, fmt.Errorf("expected string, got %T", item)
			}
		}
		return result, nil
	default:
		return nil, fmt.Errorf("expected string or array, got %T", v)
	}
}

// ValidateEmail 验证邮箱地址
func ValidateEmail(email string) bool {
	_, err := mail.ParseAddress(email)
	return err == nil
}
