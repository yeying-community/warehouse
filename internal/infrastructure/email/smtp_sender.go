package email

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"html/template"
	"mime"
	"net/smtp"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yeying-community/webdav/internal/infrastructure/config"
	"go.uber.org/zap"
)

// Sender SMTP 邮件发送器
type Sender struct {
	cfg     config.EmailConfig
	logger  *zap.Logger
	tplOnce sync.Once
	tpl     *template.Template
	tplErr  error
	subject string
}

// NewSender 创建邮件发送器
func NewSender(cfg config.EmailConfig, logger *zap.Logger) *Sender {
	return &Sender{
		cfg:     cfg,
		logger:  logger,
		subject: "登录验证码",
	}
}

// SendCode 发送登录验证码
func (s *Sender) SendCode(to string, code string, ttl time.Duration) error {
	if !s.cfg.Enabled {
		return errors.New("email login is disabled")
	}
	if s.cfg.SMTPHost == "" || s.cfg.From == "" {
		return errors.New("smtp configuration is incomplete")
	}

	body, err := s.renderTemplate(map[string]any{
		"code":      code,
		"expiresIn": int(ttl.Minutes()),
	})
	if err != nil {
		return err
	}

	msg, err := s.buildMessage(to, body)
	if err != nil {
		return err
	}

	return s.sendSMTP(to, msg)
}

func (s *Sender) renderTemplate(data map[string]any) (string, error) {
	s.tplOnce.Do(func() {
		if s.cfg.TemplatePath == "" {
			s.tplErr = errors.New("template_path is empty")
			return
		}
		path := filepath.Clean(s.cfg.TemplatePath)
		tpl, err := template.ParseFiles(path)
		if err != nil {
			s.tplErr = err
			return
		}
		s.tpl = tpl
	})

	if s.tplErr != nil {
		return "", s.tplErr
	}
	if s.tpl == nil {
		return "", errors.New("template not loaded")
	}

	var buf bytes.Buffer
	if err := s.tpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (s *Sender) buildMessage(to, body string) ([]byte, error) {
	from := s.cfg.From
	if strings.TrimSpace(s.cfg.FromName) != "" {
		encodedName := mime.QEncoding.Encode("UTF-8", s.cfg.FromName)
		from = fmt.Sprintf("%s <%s>", encodedName, s.cfg.From)
	}
	subject := mime.QEncoding.Encode("UTF-8", s.subject)

	headers := []string{
		fmt.Sprintf("From: %s", from),
		fmt.Sprintf("To: %s", to),
		fmt.Sprintf("Subject: %s", subject),
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
	}

	message := strings.Join(headers, "\r\n") + "\r\n\r\n" + body
	return []byte(message), nil
}

func (s *Sender) sendSMTP(to string, msg []byte) error {
	addr := fmt.Sprintf("%s:%d", s.cfg.SMTPHost, s.cfg.SMTPPort)
	auth := smtp.Auth(nil)
	if s.cfg.SMTPUsername != "" {
		auth = smtp.PlainAuth("", s.cfg.SMTPUsername, s.cfg.SMTPPassword, s.cfg.SMTPHost)
	}

	if s.cfg.UseTLS {
		return s.sendWithTLS(addr, auth, to, msg)
	}

	return s.sendWithStartTLS(addr, auth, to, msg)
}

func (s *Sender) sendWithTLS(addr string, auth smtp.Auth, to string, msg []byte) error {
	tlsConfig := &tls.Config{
		ServerName:         s.cfg.SMTPHost,
		InsecureSkipVerify: s.cfg.InsecureSkipVerify,
	}

	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.cfg.SMTPHost)
	if err != nil {
		return err
	}
	defer client.Close()

	if auth != nil {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(auth); err != nil {
				return err
			}
		}
	}

	if err := client.Mail(s.cfg.From); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(msg); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func (s *Sender) sendWithStartTLS(addr string, auth smtp.Auth, to string, msg []byte) error {
	client, err := smtp.Dial(addr)
	if err != nil {
		return err
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		tlsConfig := &tls.Config{
			ServerName:         s.cfg.SMTPHost,
			InsecureSkipVerify: s.cfg.InsecureSkipVerify,
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return err
		}
	}

	if auth != nil {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(auth); err != nil {
				return err
			}
		}
	}

	if err := client.Mail(s.cfg.From); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(msg); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}
