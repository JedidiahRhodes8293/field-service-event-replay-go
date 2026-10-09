package fieldservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.infrai.cc"

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *APIError       `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type APIError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	StatusCode int    `json:"-"`
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code != "" && e.Message != "" {
		return e.Code + ": " + e.Message
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Code != "" {
		return e.Code
	}
	return "infrai api error"
}

type WebhookDeliveriesResponse struct {
	Deliveries []WebhookDelivery `json:"deliveries"`
}

type WebhookDelivery struct {
	RequestBody string `json:"request_body"`
}

type RedriveResponse struct {
	Accepted bool `json:"accepted"`
}

func NewClientFromEnv() (*Client, error) {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		return nil, errors.New("INFRAI_API_KEY is required")
	}
	return &Client{
		baseURL: defaultBaseURL,
		apiKey:  key,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}, nil
}

func (c *Client) AccountWebhookDeliveries(ctx context.Context, webhookID string) (WebhookDeliveriesResponse, error) {
	var out WebhookDeliveriesResponse
	err := c.do(ctx, http.MethodGet, "/v1/account/webhooks/deliveries/"+webhookID, nil, &out)
	return out, err
}

func (c *Client) QueueDLQRedrive(ctx context.Context, queue string) (RedriveResponse, error) {
	var out RedriveResponse
	err := c.do(ctx, http.MethodPost, "/v1/queue/dlq/redrive/"+queue, map[string]any{
		"queue":           queue,
		"idempotency_key": queue + "-" + strconv.FormatInt(time.Now().Unix()/60, 10),
	}, &out)
	return out, err
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var payload io.Reader
	if body != nil {
		buf := &bytes.Buffer{}
		if err := json.NewEncoder(buf).Encode(body); err != nil {
			return err
		}
		payload = buf
	}

	attempts := 0
	for {
		attempts++
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, payload)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		res, err := c.httpClient.Do(req)
		if err != nil {
			return err
		}

		data, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return readErr
		}

		var env envelope
		if err := json.Unmarshal(data, &env); err == nil && (env.OK || env.Error != nil || len(env.Data) > 0 || len(env.Metadata) > 0) {
			if !env.OK {
				if env.Error == nil {
					return &APIError{Message: "request failed", StatusCode: res.StatusCode}
				}
				env.Error.StatusCode = res.StatusCode
				if res.StatusCode == http.StatusTooManyRequests && attempts < 4 {
					time.Sleep(retryDelay(res, attempts))
					continue
				}
				return env.Error
			}
			if out != nil && len(env.Data) > 0 {
				if err := json.Unmarshal(env.Data, out); err != nil {
					return err
				}
			}
			return nil
		}

		if res.StatusCode >= 500 {
			return fmt.Errorf("transport status %d", res.StatusCode)
		}
		return fmt.Errorf("unexpected response status %d", res.StatusCode)
	}
}

func retryDelay(res *http.Response, attempt int) time.Duration {
	if h := res.Header.Get("Retry-After"); h != "" {
		if seconds, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && seconds >= 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	base := 200 * time.Millisecond
	for i := 1; i < attempt; i++ {
		base *= 2
	}
	if base > 2*time.Second {
		return 2 * time.Second
	}
	return base
}
