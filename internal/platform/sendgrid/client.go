package sendgrid

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/envutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/httpx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type Client interface {
	Send(ctx context.Context, req SendEmailRequest) (*SendEmailResult, error)
}

type Config struct {
	APIKey           string
	BaseURL          string
	DefaultFromEmail string
	DefaultFromName  string
	Timeout          time.Duration
	MaxRetries       int
}

func ConfigFromEnv() Config {
	timeoutSec := envutil.Int("SENDGRID_TIMEOUT_SECONDS", 30)
	maxRetries := envutil.Int("SENDGRID_MAX_RETRIES", 4)
	apiKey := strings.TrimSpace(os.Getenv("SENDGRID_API_KEY"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("SENGRID_API_KEY"))
	}
	fromName := strings.TrimSpace(os.Getenv("SENDGRID_FROM_NAME"))
	if fromName == "" {
		fromName = strings.TrimSpace(os.Getenv("SENGRID_FROM_NAME"))
	}

	return Config{
		APIKey:           apiKey,
		BaseURL:          strings.TrimSpace(os.Getenv("SENDGRID_BASE_URL")),
		DefaultFromEmail: strings.TrimSpace(os.Getenv("SENDGRID_FROM_EMAIL")),
		DefaultFromName:  fromName,
		Timeout:          time.Duration(timeoutSec) * time.Second,
		MaxRetries:       maxRetries,
	}
}

func NewFromEnv(log *logger.Logger) (Client, error) {
	return New(log, ConfigFromEnv())
}

func New(log *logger.Logger, cfg Config) (Client, error) {
	if log == nil {
		return nil, fmt.Errorf("logger required")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("missing SENDGRID_API_KEY")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = "https://api.sendgrid.com"
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")

	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 4
	}

	return &client{
		log:        log.With("client", "SendGridClient"),
		cfg:        cfg,
		httpClient: &http.Client{Timeout: cfg.Timeout},
		maxRetries: cfg.MaxRetries,
	}, nil
}

type client struct {
	log        *logger.Logger
	cfg        Config
	httpClient *http.Client
	maxRetries int
}

// --- public request/response types ---

type EmailAddress struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

type Attachment struct {
	Filename      string
	MIMEType      string
	Content       []byte
	ContentBase64 string
	Disposition   string
	ContentID     string
}

type SendEmailRequest struct {
	From                EmailAddress
	ReplyTo             *EmailAddress
	To                  []EmailAddress
	CC                  []EmailAddress
	BCC                 []EmailAddress
	Subject             string
	Text                string
	HTML                string
	TemplateID          string
	DynamicTemplateData map[string]any
	Categories          []string
	Headers             map[string]string
	CustomArgs          map[string]string
	SendAt              *time.Time
	Attachments         []Attachment
}

type SendEmailResult struct {
	StatusCode int
	MessageID  string
	RequestID  string
}

// --- SendGrid mail send wire types ---
type mailSendRequest struct {
	Personalizations []personalization `json:"personalizations"`
	From             EmailAddress      `json:"from"`
	ReplyTo          *EmailAddress     `json:"reply_to,omitempty"`
	Subject          string            `json:"subject,omitempty"`
	Content          []mailContent     `json:"content,omitempty"`
	TemplateID       string            `json:"template_id,omitempty"`
	Categories       []string          `json:"categories,omitempty"`
	Headers          map[string]string `json:"headers,omitempty"`
	CustomArgs       map[string]string `json:"custom_args,omitempty"`
	SendAt           *int64            `json:"send_at,omitempty"`
	Attachments      []sgAttachment    `json:"attachments,omitempty"`
}

type personalization struct {
	To                  []EmailAddress    `json:"to"`
	Cc                  []EmailAddress    `json:"cc,omitempty"`
	Bcc                 []EmailAddress    `json:"bcc,omitempty"`
	DynamicTemplateData map[string]any    `json:"dynamic_template_data,omitempty"`
	Headers             map[string]string `json:"headers,omitempty"`
	CustomArgs          map[string]string `json:"curstom_args,omitempty"`
	SendAt              *int64            `json:"send_at,omitempty"`
}

type mailContent struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type sgAttachment struct {
	Content     string `json:"content"`
	Type        string `json:"type,omitempty"`
	Filename    string `json:"filename"`
	Disposition string `json:"disposition,omitempty"`
	ContentID   string `json:"content_id,omitempty"`
}

func (c *client) Send(ctx context.Context, req SendEmailRequest) (*SendEmailResult, error) {
	if c == nil || c.httpClient == nil {
		return nil, fmt.Errorf("sengrid client unavailable")
	}

	if strings.TrimSpace(req.From.Email) == "" {
		req.From.Email = c.cfg.DefaultFromEmail
		if strings.TrimSpace(req.From.Name) == "" {
			req.From.Name = c.cfg.DefaultFromName
		}
	}

	req.From.Email = strings.TrimSpace(req.From.Email)
	req.From.Name = strings.TrimSpace(req.From.Name)
	if req.ReplyTo != nil {
		req.ReplyTo.Email = strings.TrimSpace(req.ReplyTo.Email)
		req.ReplyTo.Name = strings.TrimSpace(req.ReplyTo.Name)
	}
	req.Subject = strings.TrimSpace(req.Subject)
	req.TemplateID = strings.TrimSpace(req.TemplateID)

	if req.From.Email == "" {
		return nil, fmt.Errorf("sendgrid: From.Email required (or set SENDGRID_FROM_EMAIL)")
	}
	if len(req.To) == 0 {
		return nil, fmt.Errorf("sendgrid: To required")
	}

	contents := []mailContent{}
	if t := strings.TrimSpace(req.Text); t != "" {
		contents = append(contents, mailContent{Type: "text/plain", Value: t})
	}
	if h := strings.TrimSpace(req.HTML); h != "" {
		contents = append(contents, mailContent{Type: "text/html", Value: h})
	}

	if req.TemplateID == "" {
		if req.Subject == "" {
			return nil, fmt.Errorf("sendgrid: Subject required (unless using TemplateID)")
		}
		if len(contents) == 0 {
			return nil, fmt.Errorf("sendgrid: Text or HTML content required (unless using TemplateID)")
		}
	}

	atts, err := buildAttachments(req.Attachments)
	if err != nil {
		return nil, err
	}

	var sendAt *int64
	if req.SendAt != nil {
		v := req.SendAt.Unix()
		sendAt = &v
	}

	p := personalization{To: req.To}
	if len(req.CC) > 0 {
		p.Cc = req.CC
	}
	if len(req.BCC) > 0 {
		p.Bcc = req.BCC
	}
	if len(req.DynamicTemplateData) > 0 {
		p.DynamicTemplateData = req.DynamicTemplateData
	}
	if len(req.Headers) > 0 {
		p.Headers = req.Headers
	}
	if len(req.CustomArgs) > 0 {
		p.CustomArgs = req.CustomArgs
	}
	if sendAt != nil {
		p.SendAt = sendAt
	}

	wire := mailSendRequest{
		Personalizations: []personalization{p},
		From:             req.From,
		ReplyTo:          req.ReplyTo,
		Subject:          req.Subject,
		Content:          contents,
		TemplateID:       req.TemplateID,
		Categories:       req.Categories,
		SendAt:           sendAt,
		Attachments:      atts,
	}

	resp, _, err := c.do(ctx, "POST", "/v3/mail/send", wire)
	if err != nil {
		return nil, err
	}

	return &SendEmailResult{
		StatusCode: resp.StatusCode,
		MessageID:  strings.TrimSpace(resp.Header.Get("X-Message-Id")),
		RequestID:  strings.TrimSpace(resp.Header.Get("X-Request-Id")),
	}, nil
}

func buildAttachments(in []Attachment) ([]sgAttachment, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]sgAttachment, 0, len(in))
	for _, a := range in {
		fn := strings.TrimSpace(a.Filename)
		if fn == "" {
			return nil, fmt.Errorf("sendgrid: attachment filename required")
		}

		b64 := strings.TrimSpace(a.ContentBase64)
		if b64 == "" && len(a.Content) > 0 {
			b64 = base64.StdEncoding.EncodeToString(a.Content)
		}
		if b64 == "" {
			return nil, fmt.Errorf("sendgrid: attachment %q missing content", fn)
		}

		att := sgAttachment{
			Content:  b64,
			Type:     strings.TrimSpace(a.MIMEType),
			Filename: fn,
		}
		if d := strings.TrimSpace(a.Disposition); d != "" {
			att.Disposition = d
		}
		if cid := strings.TrimSpace(a.ContentID); cid != "" {
			att.ContentID = cid
		}
		out = append(out, att)
	}
	return out, nil
}

// ---------- HTTP / retry helpers ----------

type errorItem struct {
	Message string `json:"message"`
	Field   any    `json:"field,omitempty"`
	Help    any    `json:"help,omitempty"`
	ID      string `json:"id,omitempty"`
}

type errorResponse struct {
	Errors []errorItem `json:"errors"`
}

type HTTPError struct {
	StatusCode int
	Body       string
	Errors     []errorItem
}

func (e *HTTPError) Error() string {
	if e == nil {
		return "sendgrid: <nil error>"
	}
	if len(e.Errors) > 0 && strings.TrimSpace(e.Errors[0].Message) != "" {
		return fmt.Sprintf("sendgrid http %d: %s", e.StatusCode, e.Errors[0].Message)
	}
	msg := strings.TrimSpace(e.Body)
	if msg == "" {
		msg = "<empty body>"
	}
	if len(msg) > 4000 {
		msg = msg[:4000] + "..."
	}
	return fmt.Sprintf("sendgrid http %d: %s", e.StatusCode, msg)
}

func (e *HTTPError) HTTPStatusCode() int {
	if e == nil {
		return 0
	}
	return e.StatusCode
}

func (c *client) do(ctx context.Context, method, path string, body any) (*http.Response, []byte, error) {
	backoff := 1 * time.Second

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}

		resp, raw, err := c.doOnce(ctx, method, path, body)
		if err == nil {
			return resp, raw, nil
		}

		if !httpx.IsRetryableError(err) || attempt == c.maxRetries {
			return nil, nil, err
		}

		sleepFor := httpx.RetryAfterDuration(resp, backoff, 10*time.Second)
		sleepFor = httpx.JitterSleep(sleepFor)

		c.log.Warn("Sendgrid request retrying",
			"path", path,
			"attempt", attempt+1,
			"max_retries", c.maxRetries,
			"sleep", sleepFor.String(),
			"error", err.Error(),
		)

		time.Sleep(sleepFor)
		backoff *= 2
	}

	return nil, nil, errors.New("unreachable retry loop")
}

func (c *client) doOnce(ctx context.Context, method, path string, body any) (*http.Response, []byte, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, nil, err
		}
	}

	req, err := http.NewRequestWithContext(ctxutil.Default(ctx), method, c.cfg.BaseURL+path, &buf)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}

	raw, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return resp, nil, readErr
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		he := &HTTPError{StatusCode: resp.StatusCode, Body: string(raw)}

		var er errorResponse
		if json.Unmarshal(raw, &er) == nil && len(er.Errors) > 0 {
			he.Errors = er.Errors
		}
		return resp, raw, he
	}

	return resp, raw, nil
}
