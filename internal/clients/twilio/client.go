package twilio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yungbote/neurobridge-backend/internal/pkg/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/pkg/envutil"
	"github.com/yungbote/neurobridge-backend/internal/pkg/httpx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type Client interface {
	SendMessage(ctx context.Context, req SendMessageRequest) (*Message, error)
	SendSMS(ctx context.Context, to string, body string) (*Message, error)
}

type Config struct {
	AccountSID											string
	AuthToken												string
	APIKey													string
	APIKeySecret										string
	BaseURL													string
	DefaultFrom											string
	DefaultMessagingServiceSID			string
	DefaultStatusCallbackURL				string
	Timeout													time.Duration
	MaxRetries											int
}

func ConfigFromEnv() Config {
	timeoutSec := envutil.Int("TWILIO_TIMEOUT_SECONDS", 30)
	maxRetries := envutil.Int("TWILIO_MAX_RETRIES", 4)

	return Config{
		AccountSID:									strings.TrimSpace(os.Getenv("TWILIO_ACCOUNT_SID")),
		AuthToken:									strings.TrimSpace(os.Getenv("TWILIO_AUTH_TOKEN")),
		APIKey:											strings.TrimSpace(os.Getenv("TWILIO_API_KEY")),
		APIKeySecret:								strings.TrimSpace(os.Getenv("TWILIO_API_KEY_SECRET")),
		BaseURL:										strings.TrimSpace(os.Getenv("TWILIO_BASE_URL")),
		DefaultFrom:								strings.TrimSpace(os.Getenv("TWILIO_FROM_NUMBER")),
		DefaultMessagingServiceSID: strings.TrimSpace(os.Getenv("TWILIO_MESSAGING_SERVICE_SID")),
		DefaultStatusCallbackURL:		strings.TrimSpace(os.Getenv("TWILIO_STAUTS_CALLBACK_URL")),
		Timeout:										time.Duration(timeoutSec) * time.Second,
		MaxRetries:									maxRetries,
	}
}

func NewFromEnv(log *logger.Logger) (Client, error) {
	return New(log, ConfigFromEnv())
}

func New(log *logger.Logger, cfg Config) (Client, error) {
	if log == nil {
		return nil, fmt.Errorf("logger required")
	}

	cfg.AccountSID = strings.TrimSpace(cfg.AccountSID)
	if cfg.AccountSID == "" {
		return nil, fmt.Errorf("missing TWILIO_ACCOUNT_SID")
	}

	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.APIKeySecret = strings.TrimSpace(cfg.APIKeySecret)
	cfg.AuthToken = strings.TrimSpace(cfg.AuthToken)
	if cfg.APIKey != "" {
		if cfg.APIKeySecret == "" {
			return nil, fmt.Errorf("missing TWILIO_API_KEY_SECRET (required when TWILIO_API_KEY is set)")
		}
	} else {
		if cfg.AuthToken == "" {
			return nil, fmt.Errorf("missing TWILIO_AUTH_TOKEN (or provide TWILIO_API_KEY + TWILIO_API_KEY_SECRET)")
		}
	}

	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = "https://api.twilio.com/2010-04-01"
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	
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
		log:				log.With("client", "TwilioClient"),
		cfg:				cfg,
		httpClient: &http.Client{Timeout: cfg.Timeout},
		maxRetries: cfg.MaxRetries,
	}, nil
}

type client struct {
	log						*logger.Logger
	cfg						Config
	httpClient		*http.Client
	maxRetries		int
}

type SendMessageRequest struct {
	To										string
	From									string
	MessagingServiceSID		string
	Body									string
	MediaURLs							[]string
	ContentSID						string
	StatusCallbackURL			string
	ApplicationSID				string
	ProvideFeedback				*bool
	ValidityPeriodSec			int
}

type Message struct {
	SID										string								`json:"sid,omitempty"`
	AccountSID						string								`json:"account_sid,omitempty"`
	To										string								`json:"to,omitempty"`
	From									string								`json:"from,omitempty"`
	Body									string								`json:"body,omitempty"`
	MessagingServiceSID		string								`json:"messaging_service_sid,omitempty"`
	Status								string								`json:"status,omitempty"`
	NumSegments						string								`json:"num_segments,omitempty"`
	ErrorCode							*int									`json:"error_code,omitempty"`
	ErrorMessage					*string								`json:"error_message,omitempty"`
	DateCreated						string								`json:"date_created,omitempty"`
	DateSent							string								`json:"date_sent,omitempty"`
	URI										string								`json:"uri,omitempty"`
}

func (c *client) SendSMS(ctx context.Context, to string, body string) (*Message, error) {
	return c.SendMessage(ctx, SendMessageRequest{
		To:		to,
		Body:	body,
	})
}

func (c *client) SendMessage(ctx context.Context, req SendMessageRequest) (*Message, error) {
	if c == nil || c.httpClient == nil {
		return nil, fmt.Errorf("twilio client unavailable")
	}

	req.To = strings.TrimSpace(req.To)
	req.From = strings.TrimSpace(req.From)
	req.MessagingServiceSID = strings.TrimSpace(req.MessagingServiceSID)
	req.Body = strings.TrimSpace(req.Body)
	req.ContentSID = strings.TrimSpace(req.ContentSID)
	req.StatusCallbackURL = strings.TrimSpace(req.StatusCallbackURL)
	req.ApplicationSID = strings.TrimSpace(req.ApplicationSID)

	if req.To == "" {
		return nil, fmt.Errorf("twilio: To required")
	}

	if req.From == "" {
		req.From = strings.TrimSpace(c.cfg.DefaultFrom)
	}
	if req.MessagingServiceSID == "" {
		req.MessagingServiceSID = strings.TrimSpace(c.cfg.DefaultMessagingServiceSID)
	}
	if req.StatusCallbackURL == "" {
		req.StatusCallbackURL = strings.TrimSpace(c.cfg.DefaultStatusCallbackURL)
	}

	if req.From == "" && req.MessagingServiceSID == "" {
		return nil, fmt.Errorf("twilio: sender required (From or MessagingServiceSID)")
	}

	hasMedia := false
	for _, u := range req.MediaURLs {
		if strings.TrimSpace(u) != "" {
			hasMedia = true
			break
		}
	}
	if req.Body == "" && !hasMedia && req.ContentSID == "" {
		return nil, fmt.Errorf("twilio: content required (Body, MediaURLs, or ContentSID)")
	}

	form := url.Values{}
	form.Set("To", req.To)
	if req.From != "" {
		form.Set("From", req.From)
	}
	if req.MessagingServiceSID != "" {
		form.Set("MessagingServiceSid", req.MessagingServiceSID)
	}
	if req.Body != "" {
		form.Set("Body", req.Body)
	}
	for _, mu := range req.MediaURLs {
		mu = strings.TrimSpace(mu)
		if mu == "" {
			continue
		}
		form.Add("MediaUrl", mu)
	}
	if req.ContentSID != "" {
		form.Set("ContentSid", req.ContentSID)
	}
	if req.StatusCallbackURL != "" {
		form.Set("StatusCallback", req.StatusCallbackURL)
	}
	if req.ApplicationSID != "" {
		form.Set("ApplicationSid", req.ApplicationSID)
	}
	if req.ProvideFeedback != nil {
		form.Set("ProvideFeedback", strconv.FormatBool(*req.ProvideFeedback))
	}
	if req.ValidityPeriodSec > 0 {
		form.Set("ValidityPeriod", strconv.Itoa(req.ValidityPeriodSec))
	}

	endpoint := fmt.Sprintf("%s/Accounts/%s/Messages.json", c.cfg.BaseURL, c.cfg.AccountSID)
	return doForm[Message](c, ctx, "POST", endpoint, form)
}

// ---------- HTTP / retry helpers ----------

type apiError struct {
	Code				int					`json:"code"`
	Message			string			`json:"message"`
	MoreInfo		string			`json:"more_info"`
	Status			int					`json:"status"`
}

type HTTPError struct {
	StatusCode	int
	Body				string
	APIError		*apiError
}

func (e *HTTPError) Error() string {
	if e == nil {
		return "twilio: <nil error>"
	}
	if e.APIError != nil && strings.TrimSpace(e.APIError.Message) != "" {
		if e.APIError.Code != 0 {
			return fmt.Sprintf("twilio http %d: %s (code=%d)", e.StatusCode, e.APIError.Message, e.APIError.Code)
		}
		return fmt.Sprintf("twilio http %d: %s", e.StatusCode, e.APIError.Message)
	}
	msg := strings.TrimSpace(e.Body)
	if msg == "" {
		msg = "<empty body>"
	}
	if len(msg) > 4000 {
		msg = msg[:4000] + "..."
	}
	return fmt.Sprintf("twilio http %d: %s", e.StatusCode, msg)
}

func (e *HTTPError) HTTPStatusCode() int {
	if e == nil {
		return 0
	}
	return e.StatusCode
}

func (c *client) basicAuth() (user, pass string) {
	if c.cfg.APIKey != "" {
		return c.cfg.APIKey, c.cfg.APIKeySecret
	}
	return c.cfg.AccountSID, c.cfg.AuthToken
}

func doForm[T any](c *client, ctx context.Context, method, urlStr string, form url.Values) (*T, error) {
	backoff := 1 * time.Second

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		out, resp, err := doFormOnce[T](c, ctx, method, urlStr, form)
		if err == nil {
			return out, nil
		}

		if !httpx.IsRetryableError(err) || attempt == c.maxRetries {
			return nil, err
		}

		sleepFor := httpx.RetryAfterDuration(resp, backoff, 10*time.Second)
		sleepFor = httpx.JitterSleep(sleepFor)

		c.log.Warn("Twilio request retrying",
			"url",					urlStr,
			"attempt",			attempt+1,
			"max_retries",	c.maxRetries,
			"sleep",				sleepFor.String(),
			"error",				err.Error(),
		)

		time.Sleep(sleepFor)
		backoff *= 2
	}

	return nil, fmt.Errorf("unreachable retry loop")
}

func doFormOnce[T any](c *client, ctx context.Context, method, urlStr string, form url.Values) (*T, *http.Response, error) {
	req, err := http.NewRequestWithContext(ctxutil.Default(ctx), method, urlStr, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	u, p := c.basicAuth()
	req.SetBasicAuth(u, p)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, resp, err
	}

	raw, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, resp, readErr
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var ae apiError
		if json.Unmarshal(raw, &ae) == nil && strings.TrimSpace(ae.Message) != "" {
			return nil, resp, &HTTPError{StatusCode: resp.StatusCode, Body: string(raw), APIError: &ae}
		}
		return nil, resp, &HTTPError{StatusCode: resp.StatusCode, Body: string(raw)}
	}

	var out T
	if len(raw) == 0 {
		return &out, resp, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, resp, fmt.Errorf("twilio decode error: %w; raw=%s", err, string(raw))
	}
	return &out, resp, nil
}
